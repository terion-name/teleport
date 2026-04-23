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

package web

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gravitational/trace"
	"github.com/julienschmidt/httprouter"

	"github.com/gravitational/teleport/api/client/proto"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/httplib"
	"github.com/gravitational/teleport/lib/reversetunnelclient"
	"github.com/gravitational/teleport/lib/web/ui"
)

// createAccessRequestPayload is the body for POST /webapi/sites/:site/accessrequests.
type createAccessRequestPayload struct {
	// Roles is the list of roles being requested.
	Roles []string `json:"roles"`
	// Resources is the list of resources being requested (for resource-access requests).
	Resources []ui.AccessRequestResourceID `json:"resources"`
	// RequestReason is the reason supplied by the requester.
	RequestReason string `json:"requestReason"`
	// SuggestedReviewers is the list of suggested reviewers.
	SuggestedReviewers []string `json:"suggestedReviewers"`
	// MaxDuration is the requested maximum duration (RFC3339).
	MaxDuration *time.Time `json:"maxDuration,omitempty"`
	// RequestTTL is the requested request TTL (RFC3339).
	RequestTTL *time.Time `json:"requestTTL,omitempty"`
	// AssumeStartTime is the time the requester wants to start using the role(s).
	AssumeStartTime *time.Time `json:"assumeStartTime,omitempty"`
	// DryRun, when true, asks the server to validate the request and return a fully populated
	// request (durations, reason mode, etc.) without persisting anything.
	DryRun bool `json:"dryRun,omitempty"`
	// RequestKind selects short-term (1) or long-term (2) access. Defaults to short-term.
	RequestKind int32 `json:"requestKind,omitempty"`
}

// submitAccessReviewPayload is the body for POST /webapi/sites/:site/accessrequests/:id/review.
type submitAccessReviewPayload struct {
	// State is the proposed state: "APPROVED", "DENIED" or "PROMOTED".
	State string `json:"state"`
	// Reason is the reviewer's reason.
	Reason string `json:"reason"`
	// Roles is an optional subset of the requested roles to approve.
	Roles []string `json:"roles"`
	// AssumeStartTime overrides the requester's assume start time.
	AssumeStartTime *time.Time `json:"assumeStartTime,omitempty"`
	// PromotedAccessListName, when non-empty, promotes the request via the named access list.
	// Only meaningful when State == "PROMOTED".
	PromotedAccessListName string `json:"promotedAccessListName,omitempty"`
}

// requestableResourceRolesPayload is the body for POST /webapi/sites/:site/requestableroles/resources.
type requestableResourceRolesPayload struct {
	// ResourceIds identifies the resources the user wants access to.
	ResourceIds []ui.AccessRequestResourceID `json:"resourceIds"`
	// Login is an optional host login to scope the requestable roles by.
	Login string `json:"login,omitempty"`
	// FilterRequestableRolesByResource, when true, returns only roles that allow access to
	// the provided resources.
	FilterRequestableRolesByResource bool `json:"filterRequestableRolesByResource,omitempty"`
}

// requestableResourceRolesResponse is the response body.
type requestableResourceRolesResponse struct {
	// ApplicableRolesForResources is the list of roles that grant access to the resources.
	ApplicableRolesForResources []string `json:"applicableRolesForResources"`
	// RequestableRoles is the list of roles the user can request.
	RequestableRoles []string `json:"requestableRoles"`
}

// requestableRoleItem is a single entry in the GET /requestableroles response.
type requestableRoleItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// requestableRolesResponse is the paginated response for GET /requestableroles.
type requestableRolesResponse struct {
	Roles         []requestableRoleItem `json:"roles"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
}

// getAccessRequests returns the list of access requests visible to the caller.
// Optional query parameters:
//   - state: "PENDING", "APPROVED", "DENIED", "PROMOTED"
//   - scope: "MY_REQUESTS", "NEEDS_REVIEW", "REVIEWED"
//   - user:  filter by requestor username
func (h *Handler) getAccessRequests(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	filter := types.AccessRequestFilter{}
	if v := r.URL.Query().Get("state"); v != "" {
		state, ok := types.RequestState_value[v]
		if !ok {
			return nil, trace.BadParameter("invalid state %q", v)
		}
		filter.State = types.RequestState(state)
	}
	if v := r.URL.Query().Get("scope"); v != "" {
		scope, ok := types.AccessRequestScope_value[v]
		if !ok {
			return nil, trace.BadParameter("invalid scope %q", v)
		}
		filter.Scope = types.AccessRequestScope(scope)
	}
	if v := r.URL.Query().Get("user"); v != "" {
		filter.User = v
	}

	items, err := clt.GetAccessRequests(r.Context(), filter)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return struct {
		Items []ui.AccessRequest `json:"items"`
	}{Items: ui.MakeAccessRequests(items)}, nil
}

// getAccessRequest returns a single access request by ID.
func (h *Handler) getAccessRequest(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	id := p.ByName("requestId")
	if id == "" {
		return nil, trace.BadParameter("missing request ID")
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	items, err := clt.GetAccessRequests(r.Context(), types.AccessRequestFilter{ID: id})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if len(items) == 0 {
		return nil, trace.NotFound("access request %q not found", id)
	}

	return ui.MakeAccessRequest(items[0]), nil
}

// createAccessRequest creates (or dry-runs) an access request.
func (h *Handler) createAccessRequest(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	var payload createAccessRequestPayload
	if err := httplib.ReadResourceJSON(r, &payload); err != nil {
		return nil, trace.Wrap(err)
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	req, err := types.NewAccessRequest(uuid.NewString(), sctx.GetUser(), payload.Roles...)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if len(payload.Resources) > 0 {
		ids := make([]types.ResourceID, 0, len(payload.Resources))
		for _, rid := range payload.Resources {
			ids = append(ids, types.ResourceID{
				ClusterName:     rid.ClusterName,
				Kind:            rid.Kind,
				Name:            rid.Name,
				SubResourceName: rid.SubResourceName,
			})
		}
		req.SetRequestedResourceIDs(ids)
	}

	req.SetRequestReason(payload.RequestReason)
	if len(payload.SuggestedReviewers) > 0 {
		req.SetSuggestedReviewers(payload.SuggestedReviewers)
	}
	if payload.MaxDuration != nil {
		req.SetMaxDuration(*payload.MaxDuration)
	}
	if payload.RequestTTL != nil {
		req.SetExpiry(*payload.RequestTTL)
	}
	if payload.AssumeStartTime != nil {
		req.SetAssumeStartTime(*payload.AssumeStartTime)
	}
	if payload.RequestKind != 0 {
		req.SetRequestKind(types.AccessRequestKind(payload.RequestKind))
	}
	req.SetDryRun(payload.DryRun)

	created, err := clt.CreateAccessRequestV2(r.Context(), req)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.MakeAccessRequest(created), nil
}

// deleteAccessRequest deletes an access request.
func (h *Handler) deleteAccessRequest(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	id := p.ByName("requestId")
	if id == "" {
		return nil, trace.BadParameter("missing request ID")
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if err := clt.DeleteAccessRequest(r.Context(), id); err != nil {
		return nil, trace.Wrap(err)
	}
	return OK(), nil
}

// submitAccessReview applies a review to an access request.
func (h *Handler) submitAccessReview(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	id := p.ByName("requestId")
	if id == "" {
		return nil, trace.BadParameter("missing request ID")
	}

	var payload submitAccessReviewPayload
	if err := httplib.ReadResourceJSON(r, &payload); err != nil {
		return nil, trace.Wrap(err)
	}

	state, ok := types.RequestState_value[payload.State]
	if !ok {
		return nil, trace.BadParameter("invalid review state %q", payload.State)
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	review := types.AccessReview{
		ProposedState: types.RequestState(state),
		Reason:        payload.Reason,
		Roles:         payload.Roles,
	}
	if payload.AssumeStartTime != nil {
		review.AssumeStartTime = payload.AssumeStartTime
	}
	if payload.PromotedAccessListName != "" {
		review.AccessList = &types.PromotedAccessList{
			Name: payload.PromotedAccessListName,
		}
	}

	updated, err := clt.SubmitAccessReview(r.Context(), types.AccessReviewSubmission{
		RequestID: id,
		Review:    review,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return ui.MakeAccessRequest(updated), nil
}

// listRequestableRoles returns the list of roles the caller can request.
func (h *Handler) listRequestableRoles(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	resp, err := clt.ListRequestableRoles(r.Context(), &proto.ListRequestableRolesRequest{})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	items := make([]requestableRoleItem, 0, len(resp.GetRoles()))
	for _, role := range resp.GetRoles() {
		items = append(items, requestableRoleItem{
			Name:        role.GetName(),
			Description: role.GetDescription(),
		})
	}
	return requestableRolesResponse{
		Roles:         items,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

// getRequestableResourceRoles returns the roles applicable to a set of resources.
func (h *Handler) getRequestableResourceRoles(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	var payload requestableResourceRolesPayload
	if err := httplib.ReadResourceJSON(r, &payload); err != nil {
		return nil, trace.Wrap(err)
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	ids := make([]types.ResourceID, 0, len(payload.ResourceIds))
	for _, rid := range payload.ResourceIds {
		ids = append(ids, types.ResourceID{
			ClusterName:     rid.ClusterName,
			Kind:            rid.Kind,
			Name:            rid.Name,
			SubResourceName: rid.SubResourceName,
		})
	}

	caps, err := clt.GetAccessCapabilities(r.Context(), types.AccessCapabilitiesRequest{
		ResourceIDs:                      ids,
		RequestableRoles:                 true,
		Login:                            payload.Login,
		FilterRequestableRolesByResource: payload.FilterRequestableRolesByResource,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return requestableResourceRolesResponse{
		ApplicableRolesForResources: caps.ApplicableRolesForResources,
		RequestableRoles:            caps.RequestableRoles,
	}, nil
}

// getAccessRequestSuggestedAccessLists returns the access list names that can promote
// the given request. Promotions are persisted at request creation by the auth server;
// this endpoint reads them back via GetAccessRequestAllowedPromotions.
func (h *Handler) getAccessRequestSuggestedAccessLists(w http.ResponseWriter, r *http.Request, p httprouter.Params, sctx *SessionContext, cluster reversetunnelclient.Cluster) (any, error) {
	id := p.ByName("requestId")
	if id == "" {
		return nil, trace.BadParameter("missing request ID")
	}

	clt, err := sctx.GetUserClient(r.Context(), cluster)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	items, err := clt.GetAccessRequests(r.Context(), types.AccessRequestFilter{ID: id})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if len(items) == 0 {
		return nil, trace.NotFound("access request %q not found", id)
	}

	promotions, err := clt.GetAccessRequestAllowedPromotions(r.Context(), items[0])
	if err != nil {
		return nil, trace.Wrap(err)
	}

	type suggested struct {
		Name string `json:"name"`
	}
	out := make([]suggested, 0)
	if promotions != nil {
		for _, p := range promotions.Promotions {
			out = append(out, suggested{Name: p.AccessListName})
		}
	}
	return struct {
		AccessLists []suggested `json:"accessLists"`
	}{AccessLists: out}, nil
}

