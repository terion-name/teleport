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

package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/api/types/accesslist"
	"github.com/gravitational/teleport/api/types/header"
	"github.com/gravitational/teleport/lib/auth/authtest"
	"github.com/gravitational/teleport/lib/modules/modulestest"
	"github.com/gravitational/teleport/lib/services"
)

// TestOSSAccessRequestWithPromotions exercises the full create -> approve / promote
// flow under OSS defaults, ensuring the entitlement flip and the promotions algorithm
// both behave as intended without enterprise-specific code paths.
func TestOSSAccessRequestWithPromotions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	testAuthServer, err := authtest.NewAuthServer(authtest.AuthServerConfig{
		Dir:     t.TempDir(),
		Modules: modulestest.OSSModules(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testAuthServer.Close()) })

	tlsServer, err := testAuthServer.NewTestTLSServer()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tlsServer.Close()) })

	auth := tlsServer.Auth()

	// Create roles:
	// - "access"        -> target role users can end up with
	// - "requester"     -> allow.request.roles=[access]
	// - "reviewer"      -> allow.review_requests.roles=[access]
	roleSpecs := map[string]types.RoleSpecV6{
		"access": {},
		"requester": {
			Allow: types.RoleConditions{
				Request: &types.AccessRequestConditions{
					Roles: []string{"access"},
				},
			},
		},
		"reviewer": {
			Allow: types.RoleConditions{
				ReviewRequests: &types.AccessReviewConditions{
					Roles: []string{"access"},
				},
			},
		},
	}
	for name, spec := range roleSpecs {
		role, err := types.NewRole(name, spec)
		require.NoError(t, err, "creating role %q", name)
		_, err = auth.UpsertRole(ctx, role)
		require.NoError(t, err, "upserting role %q", name)
	}

	users := map[string][]string{
		"alice": {"requester"},
		"bob":   {"reviewer"},
	}
	for name, roles := range users {
		u, err := types.NewUser(name)
		require.NoError(t, err)
		u.SetRoles(roles)
		_, err = auth.UpsertUser(ctx, u)
		require.NoError(t, err)
	}

	// Create an access list whose grants cover the requested role and where alice is NOT a member.
	list, err := accesslist.NewAccessList(
		header.Metadata{Name: "covers-access"},
		accesslist.Spec{
			Title:  "Access list covering access",
			Type:   accesslist.Static,
			Owners: []accesslist.Owner{{Name: "bob"}},
			Grants: accesslist.Grants{
				Roles: []string{"access"},
			},
		},
	)
	require.NoError(t, err)
	_, err = auth.UpsertAccessList(ctx, list)
	require.NoError(t, err)

	// Create access request as alice.
	req, err := services.NewAccessRequest("alice", "access")
	require.NoError(t, err)
	req.SetRequestReason("test")

	aliceClient, err := tlsServer.NewClient(authtest.TestUser("alice"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, aliceClient.Close()) })

	created, err := aliceClient.CreateAccessRequestV2(ctx, req)
	require.NoError(t, err, "alice creates access request")
	require.Equal(t, types.RequestState_PENDING, created.GetState())

	// Promotions should have been generated server-side at create-time.
	promotions, err := aliceClient.GetAccessRequestAllowedPromotions(ctx, created)
	require.NoError(t, err)
	require.NotNil(t, promotions)
	require.Len(t, promotions.Promotions, 1, "expected exactly one suggested promotion")
	require.Equal(t, "covers-access", promotions.Promotions[0].AccessListName)

	// Bob approves.
	bobClient, err := tlsServer.NewClient(authtest.TestUser("bob"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bobClient.Close()) })

	reviewed, err := bobClient.SubmitAccessReview(ctx, types.AccessReviewSubmission{
		RequestID: created.GetName(),
		Review: types.AccessReview{
			ProposedState: types.RequestState_APPROVED,
			Reason:        "LGTM",
		},
	})
	require.NoError(t, err, "bob approves request")
	require.Equal(t, types.RequestState_APPROVED, reviewed.GetState())
}

// TestOSSAccessRequestPromotions_NoMatchingList verifies that no promotions are
// suggested when no access list covers the requested roles.
func TestOSSAccessRequestPromotions_NoMatchingList(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	testAuthServer, err := authtest.NewAuthServer(authtest.AuthServerConfig{
		Dir:     t.TempDir(),
		Modules: modulestest.OSSModules(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testAuthServer.Close()) })

	tlsServer, err := testAuthServer.NewTestTLSServer()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tlsServer.Close()) })

	auth := tlsServer.Auth()

	for name, spec := range map[string]types.RoleSpecV6{
		"access": {},
		"auditor": {
			Allow: types.RoleConditions{Rules: []types.Rule{{Resources: []string{"*"}, Verbs: []string{"list", "read"}}}},
		},
		"requester": {
			Allow: types.RoleConditions{
				Request: &types.AccessRequestConditions{Roles: []string{"access"}},
			},
		},
	} {
		role, err := types.NewRole(name, spec)
		require.NoError(t, err)
		_, err = auth.UpsertRole(ctx, role)
		require.NoError(t, err)
	}

	u, err := types.NewUser("alice")
	require.NoError(t, err)
	u.SetRoles([]string{"requester"})
	_, err = auth.UpsertUser(ctx, u)
	require.NoError(t, err)

	// Access list exists but grants a different role (auditor), so it should NOT be suggested.
	list, err := accesslist.NewAccessList(
		header.Metadata{Name: "wrong-coverage"},
		accesslist.Spec{
			Title:  "Access list with wrong coverage",
			Type:   accesslist.Static,
			Owners: []accesslist.Owner{{Name: "admin"}},
			Grants: accesslist.Grants{
				Roles: []string{"auditor"},
			},
		},
	)
	require.NoError(t, err)
	_, err = auth.UpsertAccessList(ctx, list)
	require.NoError(t, err)

	req, err := services.NewAccessRequest("alice", "access")
	require.NoError(t, err)

	aliceClient, err := tlsServer.NewClient(authtest.TestUser("alice"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, aliceClient.Close()) })

	created, err := aliceClient.CreateAccessRequestV2(ctx, req)
	require.NoError(t, err)

	promotions, err := aliceClient.GetAccessRequestAllowedPromotions(ctx, created)
	require.NoError(t, err)
	require.NotNil(t, promotions)
	require.Empty(t, promotions.Promotions, "no promotions expected for non-covering access list")
}
