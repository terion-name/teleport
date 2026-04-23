/*
 * Teleport
 * Copyright (C) 2026  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package auth

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gravitational/trace"
	"golang.org/x/oauth2"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/api/constants"
	apidefaults "github.com/gravitational/teleport/api/defaults"
	"github.com/gravitational/teleport/api/types"
	apievents "github.com/gravitational/teleport/api/types/events"
	apiutils "github.com/gravitational/teleport/api/utils"
	"github.com/gravitational/teleport/api/utils/keys/hardwarekey"
	"github.com/gravitational/teleport/lib/auth/authclient"
	"github.com/gravitational/teleport/lib/authz"
	"github.com/gravitational/teleport/lib/client/sso"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/loginrule"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/utils"
)

type serverOIDCService struct {
	*Server
}

func newServerOIDCService(server *Server) OIDCService {
	return &serverOIDCService{Server: server}
}

func (s *serverOIDCService) CreateOIDCAuthRequest(ctx context.Context, req types.OIDCAuthRequest) (*types.OIDCAuthRequest, error) {
	return s.createOIDCAuthRequest(ctx, req, false)
}

func (s *serverOIDCService) CreateOIDCAuthRequestForMFA(ctx context.Context, req types.OIDCAuthRequest) (*types.OIDCAuthRequest, error) {
	return s.createOIDCAuthRequest(ctx, req, true)
}

func (s *serverOIDCService) createOIDCAuthRequest(ctx context.Context, req types.OIDCAuthRequest, useMFASettings bool) (*types.OIDCAuthRequest, error) {
	connector, err := s.getOIDCConnector(ctx, req, useMFASettings)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if !req.CreateWebSession {
		ceremonyType := sso.CeremonyTypeLogin
		switch {
		case useMFASettings:
			ceremonyType = sso.CeremonyTypeMFA
		case req.SSOTestFlow:
			ceremonyType = sso.CeremonyTypeTest
		}

		if err := sso.ValidateClientRedirect(req.ClientRedirectURL, ceremonyType, connector.GetClientRedirectSettings()); err != nil {
			return nil, trace.Wrap(err, InvalidClientRedirectErrorMessage)
		}
	}

	req.StateToken, err = utils.CryptoRandomHex(defaults.TokenLenBytes)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	redirectURL, err := services.GetRedirectURL(connector, req.ProxyAddress)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	req.RedirectURL = redirectURL

	config, err := s.oauth2Config(ctx, connector, redirectURL)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if connector.IsPKCEEnabled() && req.PkceVerifier == "" {
		req.PkceVerifier = oauth2.GenerateVerifier()
	}

	opts := s.authCodeOptions(connector, req)
	req.RedirectURL = config.AuthCodeURL(req.StateToken, opts...)

	if err := s.Services.CreateOIDCAuthRequest(ctx, req, defaults.OIDCAuthRequestTTL); err != nil {
		return nil, trace.Wrap(err)
	}

	return &req, nil
}

func (s *serverOIDCService) ValidateOIDCAuthCallback(ctx context.Context, q url.Values) (*authclient.OIDCAuthResponse, error) {
	diagCtx := NewSSODiagContext(types.KindOIDC, s)
	event := &apievents.UserLogin{
		Metadata: apievents.Metadata{
			Type: events.UserLoginEvent,
		},
		Method:             events.LoginMethodOIDC,
		ConnectionMetadata: authz.ConnectionMetadata(ctx),
	}

	resp, err := s.validateOIDCAuthCallback(ctx, q, diagCtx)
	diagCtx.Info.Error = trace.UserMessage(err)
	event.AppliedLoginRules = diagCtx.Info.AppliedLoginRules
	diagCtx.WriteToBackend(ctx)

	if claims := oidcClaimsToTraits(map[string]any(diagCtx.Info.OIDCClaims)); len(claims) > 0 {
		if attributes, encErr := apievents.EncodeMapStrings(claims); encErr == nil {
			event.IdentityAttributes = attributes
		} else {
			event.Status.UserMessage = fmt.Sprintf("Failed to encode identity attributes: %v", encErr.Error())
			s.logger.DebugContext(ctx, "Failed to encode OIDC identity attributes", "error", encErr)
		}
	}

	if err != nil {
		event.Code = events.UserSSOLoginFailureCode
		if diagCtx.Info.TestFlow {
			event.Code = events.UserSSOTestFlowLoginFailureCode
		}
		event.Status.Success = false
		event.Status.Error = trace.Unwrap(err).Error()
		event.Status.UserMessage = err.Error()

		if emitErr := s.emitter.EmitAuditEvent(ctx, event); emitErr != nil {
			s.logger.WarnContext(ctx, "Failed to emit OIDC login failed event", "error", emitErr)
		}
		return nil, trace.Wrap(err)
	}

	event.Code = events.UserSSOLoginCode
	if diagCtx.Info.TestFlow {
		event.Code = events.UserSSOTestFlowLoginCode
	}
	event.Status.Success = true
	event.User = resp.Username
	if emitErr := s.emitter.EmitAuditEvent(ctx, event); emitErr != nil {
		s.logger.WarnContext(ctx, "Failed to emit OIDC login event", "error", emitErr)
	}

	return resp, nil
}

func (s *serverOIDCService) validateOIDCAuthCallback(ctx context.Context, q url.Values, diagCtx *SSODiagContext) (*authclient.OIDCAuthResponse, error) {
	if errParam := q.Get("error"); errParam != "" {
		if state := q.Get("state"); state != "" {
			diagCtx.RequestID = state
			if req, err := s.Services.GetOIDCAuthRequest(ctx, state); err == nil {
				diagCtx.Info.TestFlow = req.SSOTestFlow
			}
		}

		errDesc := q.Get("error_description")
		oauthErr := trace.OAuth2("invalid_request", errParam, q)
		return nil, trace.WithUserMessage(oauthErr, "OIDC returned error: %v [%v]", errDesc, errParam)
	}

	code := q.Get("code")
	if code == "" {
		oauthErr := trace.OAuth2("invalid_request", "code query param must be set", q)
		return nil, trace.WithUserMessage(oauthErr, "Invalid parameters received from OIDC provider.")
	}

	stateToken := q.Get("state")
	if stateToken == "" {
		oauthErr := trace.OAuth2("invalid_request", "missing state query param", q)
		return nil, trace.WithUserMessage(oauthErr, "Invalid parameters received from OIDC provider.")
	}
	diagCtx.RequestID = stateToken

	req, err := s.Services.GetOIDCAuthRequest(ctx, stateToken)
	if err != nil {
		return nil, trace.Wrap(err, "Failed to get OIDC auth request.")
	}
	diagCtx.Info.TestFlow = req.SSOTestFlow

	connector, err := s.getOIDCConnector(ctx, *req, req.CheckUser)
	if err != nil {
		return nil, trace.Wrap(err, "Failed to get OIDC connector.")
	}

	token, rawIDToken, claims, idToken, err := s.exchangeOIDCToken(ctx, connector, req, code)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	_ = token
	_ = rawIDToken

	if idToken.Nonce == "" || idToken.Nonce != req.StateToken {
		return nil, trace.AccessDenied("OIDC callback contained an invalid nonce")
	}

	email, _ := oidcStringClaim(claims, "email")
	emailVerified := oidcBoolClaim(claims, "email_verified")
	if email != "" && !connector.GetAllowUnverifiedEmail() && !emailVerified {
		return nil, trace.AccessDenied("OIDC user email is not verified")
	}

	if acr := connector.GetACR(); acr != "" {
		gotACR, _ := oidcStringClaim(claims, "acr")
		if gotACR != acr {
			return nil, trace.AccessDenied("OIDC callback ACR claim did not match the configured acr_values")
		}
	}

	username, err := oidcUsernameFromClaims(connector, claims, email)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	subject, ok := oidcStringClaim(claims, "sub")
	if !ok || subject == "" {
		return nil, trace.AccessDenied("OIDC callback missing subject claim")
	}

	identity := types.ExternalIdentity{
		ConnectorID: req.ConnectorID,
		Username:    username,
		UserID:      subject,
	}

	diagCtx.Info.OIDCClaimsToRoles = connector.GetClaimsToRoles()
	diagCtx.Info.OIDCClaims = types.OIDCClaims(claims)
	diagCtx.Info.OIDCIdentity = &types.OIDCIdentity{
		ID:        subject,
		Email:     email,
		ExpiresAt: idToken.Expiry,
	}

	if req.CheckUser {
		return s.makeOIDCMFAResponse(ctx, req, identity)
	}

	params, err := s.calculateOIDCUser(ctx, diagCtx, connector, claims, identity, req)
	if err != nil {
		return nil, trace.Wrap(err, "Failed to calculate user attributes.")
	}

	user, err := s.createOIDCUser(ctx, params, req.SSOTestFlow)
	if err != nil {
		return nil, trace.Wrap(err, "Failed to create or update user.")
	}

	userState, err := s.GetUserOrLoginState(ctx, user.GetName())
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if req.SSOTestFlow {
		diagCtx.Info.Success = true
		return &authclient.OIDCAuthResponse{
			Req:      OIDCAuthRequestFromProto(req),
			Identity: identity,
			Username: params.Username,
		}, nil
	}

	return s.makeOIDCAuthResponse(ctx, req, userState, identity, params.SessionTTL)
}

func (s *serverOIDCService) makeOIDCMFAResponse(ctx context.Context, req *types.OIDCAuthRequest, identity types.ExternalIdentity) (*authclient.OIDCAuthResponse, error) {
	mfaSession, err := s.GetMFASession(ctx, req.StateToken)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	user, err := s.GetUser(ctx, mfaSession.Username, false)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	match := false
	for _, existing := range user.GetOIDCIdentities() {
		if existing.IsEqual(&identity) {
			match = true
			break
		}
	}
	if !match {
		return nil, trace.AccessDenied("OIDC callback identity does not match the user that started the MFA challenge")
	}

	token, err := s.UpsertMFASessionWithToken(ctx, mfaSession)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &authclient.OIDCAuthResponse{
		Req:      OIDCAuthRequestFromProto(req),
		Identity: identity,
		Username: user.GetName(),
		MFAToken: token,
	}, nil
}

func (s *serverOIDCService) calculateOIDCUser(ctx context.Context, diagCtx *SSODiagContext, connector types.OIDCConnector, claims map[string]any, identity types.ExternalIdentity, request *types.OIDCAuthRequest) (*CreateUserParams, error) {
	traits := oidcClaimsToTraits(claims)

	evaluationOutput, err := s.GetLoginRuleEvaluator().Evaluate(ctx, &loginrule.EvaluationInput{
		Traits: traits,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	traits = evaluationOutput.Traits
	diagCtx.Info.AppliedLoginRules = evaluationOutput.AppliedRules

	warnings, roles := services.TraitsToRoles(connector.GetTraitMappings(), traits)
	if len(warnings) > 0 {
		diagCtx.Info.OIDCClaimsToRolesWarnings = &types.SSOWarnings{
			Warnings: warnings,
		}
	}
	if len(roles) == 0 {
		return nil, trace.AccessDenied("OIDC connector did not map any claims to Teleport roles")
	}

	resolvedRoles, err := services.FetchRolesWithContext(roles, s, services.RoleTemplateContext{
		Username: identity.Username,
		Traits:   traits,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &CreateUserParams{
		ConnectorName: connector.GetName(),
		Username:      identity.Username,
		UserID:        identity.UserID,
		Roles:         roles,
		Traits:        traits,
		SessionTTL:    utils.MinTTL(resolvedRoles.AdjustSessionTTL(apidefaults.MaxCertDuration), request.CertTTL),
	}, nil
}

func (s *serverOIDCService) createOIDCUser(ctx context.Context, p *CreateUserParams, dryRun bool) (types.User, error) {
	s.logger.DebugContext(ctx, "Generating dynamic OIDC identity",
		"connector_name", p.ConnectorName,
		"user_name", p.Username,
		"roles", p.Roles,
		"dry_run", dryRun,
	)

	expires := s.GetClock().Now().UTC().Add(p.SessionTTL)
	user := &types.UserV2{
		Kind:    types.KindUser,
		Version: types.V2,
		Metadata: types.Metadata{
			Name:      p.Username,
			Namespace: apidefaults.Namespace,
			Expires:   &expires,
		},
		Spec: types.UserSpecV2{
			Roles:  p.Roles,
			Traits: p.Traits,
			OIDCIdentities: []types.ExternalIdentity{{
				ConnectorID: p.ConnectorName,
				Username:    p.Username,
				UserID:      p.UserID,
			}},
			CreatedBy: types.CreatedBy{
				User: types.UserRef{Name: teleport.UserSystem},
				Time: s.GetClock().Now().UTC(),
				Connector: &types.ConnectorRef{
					Type:     constants.OIDC,
					ID:       p.ConnectorName,
					Identity: p.Username,
				},
			},
		},
	}

	if dryRun {
		return user, nil
	}

	existingUser, err := s.Services.GetUser(ctx, p.Username, false)
	if err != nil && !trace.IsNotFound(err) {
		return nil, trace.Wrap(err)
	}
	if existingUser != nil {
		ref := user.GetCreatedBy().Connector
		if !ref.IsSameProvider(existingUser.GetCreatedBy().Connector) {
			return nil, trace.AlreadyExists("local user %q already exists and is not an OIDC user", existingUser.GetName())
		}

		match := len(existingUser.GetOIDCIdentities()) == 0
		for _, existingIdentity := range existingUser.GetOIDCIdentities() {
			if existingIdentity.IsEqual(&user.Spec.OIDCIdentities[0]) {
				match = true
				break
			}
		}
		if !match {
			return nil, trace.AlreadyExists("user %q already exists with a different OIDC identity", existingUser.GetName())
		}

		user.SetRevision(existingUser.GetRevision())
		if _, err := s.UpdateUser(ctx, user); err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		if _, err := s.CreateUser(ctx, user); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	return user, nil
}

func (s *serverOIDCService) makeOIDCAuthResponse(ctx context.Context, req *types.OIDCAuthRequest, userState services.UserState, identity types.ExternalIdentity, sessionTTL time.Duration) (*authclient.OIDCAuthResponse, error) {
	auth := authclient.OIDCAuthResponse{
		Req:      OIDCAuthRequestFromProto(req),
		Identity: identity,
		Username: userState.GetName(),
	}

	if req.CreateWebSession {
		session, err := s.CreateWebSessionFromReq(ctx, NewWebSessionRequest{
			User:                 userState.GetName(),
			Roles:                userState.GetRoles(),
			Traits:               userState.GetTraits(),
			SessionTTL:           sessionTTL,
			LoginTime:            s.clock.Now().UTC(),
			LoginIP:              req.ClientLoginIP,
			LoginUserAgent:       req.ClientUserAgent,
			AttestWebSession:     true,
			CreateDeviceWebToken: true,
			Scope:                req.Scope,
		})
		if err != nil {
			return nil, trace.Wrap(err, "Failed to create web session.")
		}
		auth.Session = session
	}

	if len(req.SshPublicKey) != 0 || len(req.TlsPublicKey) != 0 {
		sshCert, tlsCert, err := s.CreateSessionCerts(ctx, &SessionCertsRequest{
			UserState:               userState,
			SessionTTL:              sessionTTL,
			SSHPubKey:               req.SshPublicKey,
			TLSPubKey:               req.TlsPublicKey,
			SSHAttestationStatement: hardwarekey.AttestationStatementFromProto(req.SshAttestationStatement),
			TLSAttestationStatement: hardwarekey.AttestationStatementFromProto(req.TlsAttestationStatement),
			Compatibility:           req.Compatibility,
			RouteToCluster:          req.RouteToCluster,
			KubernetesCluster:       req.KubernetesCluster,
			LoginIP:                 req.ClientLoginIP,
			Scope:                   req.Scope,
		})
		if err != nil {
			return nil, trace.Wrap(err, "Failed to create session certificate.")
		}

		clusterName, err := s.GetClusterName(ctx)
		if err != nil {
			return nil, trace.Wrap(err, "Failed to obtain cluster name.")
		}

		auth.Cert = sshCert
		auth.TLSCert = tlsCert

		authority, err := s.GetCertAuthority(ctx, types.CertAuthID{
			Type:       types.HostCA,
			DomainName: clusterName.GetClusterName(),
		}, false)
		if err != nil {
			return nil, trace.Wrap(err, "Failed to obtain cluster's host CA.")
		}
		auth.HostSigners = append(auth.HostSigners, authority)
	}

	if options, err := s.ClientOptionsForLogin(userState); err == nil {
		auth.ClientOptions = options
	} else {
		logger.WarnContext(ctx, "Failed to calculate client options for OIDC login", "username", userState.GetName(), "error", err)
	}

	return &auth, nil
}

func (s *serverOIDCService) getOIDCConnector(ctx context.Context, request types.OIDCAuthRequest, useMFASettings bool) (types.OIDCConnector, error) {
	var connector types.OIDCConnector
	var err error

	if request.SSOTestFlow {
		if request.ConnectorSpec == nil {
			return nil, trace.BadParameter("ConnectorSpec cannot be nil for OIDC test flows")
		}
		connector, err = types.NewOIDCConnector(request.ConnectorID, *request.ConnectorSpec)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		connector, err = s.GetOIDCConnector(ctx, request.ConnectorID, true)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	}

	if useMFASettings {
		if err := connector.WithMFASettings(); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	return connector, nil
}

func (s *serverOIDCService) oauth2Config(ctx context.Context, connector types.OIDCConnector, redirectURL string) (*oauth2.Config, error) {
	provider, err := gooidc.NewProvider(ctx, connector.GetIssuerURL())
	if err != nil {
		return nil, trace.Wrap(err)
	}

	scopes := append([]string{gooidc.ScopeOpenID}, connector.GetScope()...)
	return &oauth2.Config{
		ClientID:     connector.GetClientID(),
		ClientSecret: connector.GetClientSecret(),
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       apiutils.Deduplicate(scopes),
	}, nil
}

func (s *serverOIDCService) exchangeOIDCToken(ctx context.Context, connector types.OIDCConnector, req *types.OIDCAuthRequest, code string) (*oauth2.Token, string, map[string]any, *gooidc.IDToken, error) {
	redirectURL, err := services.GetRedirectURL(connector, req.ProxyAddress)
	if err != nil {
		return nil, "", nil, nil, trace.Wrap(err)
	}
	config, err := s.oauth2Config(ctx, connector, redirectURL)
	if err != nil {
		return nil, "", nil, nil, trace.Wrap(err)
	}

	exchangeOpts := make([]oauth2.AuthCodeOption, 0, 1)
	if req.PkceVerifier != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(req.PkceVerifier))
	}
	token, err := config.Exchange(ctx, code, exchangeOpts...)
	if err != nil {
		return nil, "", nil, nil, trace.Wrap(err, "Failed to exchange OIDC code for token.")
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, "", nil, nil, trace.AccessDenied("OIDC provider did not return an ID token")
	}

	provider, err := gooidc.NewProvider(ctx, connector.GetIssuerURL())
	if err != nil {
		return nil, "", nil, nil, trace.Wrap(err)
	}

	idToken, err := provider.Verifier(&gooidc.Config{
		ClientID: connector.GetClientID(),
	}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, "", nil, nil, trace.Wrap(err, "Failed to verify OIDC ID token.")
	}

	claims := make(map[string]any)
	if err := idToken.Claims(&claims); err != nil {
		return nil, "", nil, nil, trace.Wrap(err, "Failed to decode OIDC ID token claims.")
	}

	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err == nil {
		userInfoClaims := make(map[string]any)
		if err := userInfo.Claims(&userInfoClaims); err == nil {
			for key, value := range userInfoClaims {
				if _, ok := claims[key]; !ok {
					claims[key] = value
				}
			}
		}
	}

	return token, rawIDToken, claims, idToken, nil
}

func (s *serverOIDCService) authCodeOptions(connector types.OIDCConnector, req types.OIDCAuthRequest) []oauth2.AuthCodeOption {
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", req.StateToken),
	}

	if req.LoginHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", req.LoginHint))
	}

	if prompt := connector.GetPrompt(); prompt != "" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", prompt))
	}

	if acr := connector.GetACR(); acr != "" {
		opts = append(opts, oauth2.SetAuthURLParam("acr_values", acr))
	}

	if maxAge, ok := connector.GetMaxAge(); ok {
		opts = append(opts, oauth2.SetAuthURLParam("max_age", strconv.FormatInt(int64(maxAge/time.Second), 10)))
	}

	if req.PkceVerifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(req.PkceVerifier))
	}

	return opts
}

// OIDCAuthRequestFromProto converts the types.OIDCAuthRequest to OIDCAuthRequest.
func OIDCAuthRequestFromProto(req *types.OIDCAuthRequest) authclient.OIDCAuthRequest {
	return authclient.OIDCAuthRequest{
		ConnectorID:       req.ConnectorID,
		CSRFToken:         req.CSRFToken,
		SSHPubKey:         req.SshPublicKey,
		TLSPubKey:         req.TlsPublicKey,
		CreateWebSession:  req.CreateWebSession,
		ClientRedirectURL: req.ClientRedirectURL,
	}
}

func oidcUsernameFromClaims(connector types.OIDCConnector, claims map[string]any, email string) (string, error) {
	usernameClaim := connector.GetUsernameClaim()
	switch {
	case usernameClaim != "":
		value, ok := oidcStringClaim(claims, usernameClaim)
		if !ok || value == "" {
			return "", trace.AccessDenied("OIDC callback missing configured username claim %q", usernameClaim)
		}
		return value, nil
	case email != "":
		return email, nil
	default:
		value, ok := oidcStringClaim(claims, "sub")
		if !ok || value == "" {
			return "", trace.AccessDenied("OIDC callback missing username and subject claims")
		}
		return value, nil
	}
}

func oidcStringClaim(claims map[string]any, key string) (string, bool) {
	value, ok := claims[key]
	if !ok {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

func oidcBoolClaim(claims map[string]any, key string) bool {
	value, ok := claims[key]
	if !ok {
		return false
	}
	b, ok := value.(bool)
	return ok && b
}

func oidcClaimsToTraits(claims map[string]any) map[string][]string {
	traits := make(map[string][]string)

	for claimName, value := range claims {
		switch claimValue := value.(type) {
		case string:
			traits[claimName] = []string{claimValue}
		case []string:
			traits[claimName] = claimValue
		case []any:
			for _, item := range claimValue {
				if str, ok := item.(string); ok {
					traits[claimName] = append(traits[claimName], str)
				}
			}
		}
	}

	return traits
}
