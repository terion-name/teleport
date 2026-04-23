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
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/api/types/accesslist"
	"github.com/gravitational/teleport/api/types/header"
	"github.com/gravitational/teleport/lib/web/ui"
)

// TestAccessRequestWebHandlers exercises the full set of OSS access-request HTTP handlers
// (list / get / create / review / delete / suggested access lists) from end-to-end,
// including the promotion suggestions exposed by the open-sourced modules algorithm.
func TestAccessRequestWebHandlers(t *testing.T) {
	t.Parallel()

	s := newWebSuite(t)
	ctx := t.Context()

	// Create roles: target, requester (can request target), reviewer (can review).
	targetRole, err := types.NewRole("ar-target", types.RoleSpecV6{})
	require.NoError(t, err)
	_, err = s.server.Auth().UpsertRole(ctx, targetRole)
	require.NoError(t, err)

	requesterRole, err := types.NewRole("ar-requester", types.RoleSpecV6{
		Allow: types.RoleConditions{
			Request: &types.AccessRequestConditions{
				Roles: []string{"ar-target"},
			},
			Rules: []types.Rule{
				{
					Resources: []string{types.KindAccessRequest},
					Verbs:     []string{types.VerbDelete},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = s.server.Auth().UpsertRole(ctx, requesterRole)
	require.NoError(t, err)

	reviewerRole, err := types.NewRole("ar-reviewer", types.RoleSpecV6{
		Allow: types.RoleConditions{
			ReviewRequests: &types.AccessReviewConditions{
				Roles: []string{"ar-target"},
			},
		},
	})
	require.NoError(t, err)
	_, err = s.server.Auth().UpsertRole(ctx, reviewerRole)
	require.NoError(t, err)

	// Pre-seed an access list that covers ar-target so promotions get suggested.
	list, err := accesslist.NewAccessList(
		header.Metadata{Name: "cover-target"},
		accesslist.Spec{
			Title:  "Access list covering ar-target",
			Type:   accesslist.Static,
			Owners: []accesslist.Owner{{Name: "admin"}},
			Grants: accesslist.Grants{Roles: []string{"ar-target"}},
		},
	)
	require.NoError(t, err)
	_, err = s.server.Auth().UpsertAccessList(ctx, list)
	require.NoError(t, err)

	// Log in as requester and reviewer (separate sessions).
	requester := s.authPack(t, "alice-req", "ar-requester")
	reviewer := s.authPack(t, "bob-rev", "ar-reviewer")

	clusterName := s.server.ClusterName()

	// List must be empty to start.
	{
		ep := requester.clt.Endpoint("webapi", "sites", clusterName, "accessrequests")
		resp, err := requester.clt.Get(ctx, ep, url.Values{})
		require.NoError(t, err)
		var out struct {
			Items []ui.AccessRequest `json:"items"`
		}
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		require.Empty(t, out.Items)
	}

	// Requester lists requestable roles and sees ar-target.
	{
		ep := requester.clt.Endpoint("webapi", "sites", clusterName, "requestableroles")
		resp, err := requester.clt.Get(ctx, ep, url.Values{})
		require.NoError(t, err)
		var out struct {
			Roles []struct {
				Name string `json:"name"`
			} `json:"roles"`
		}
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		names := make([]string, 0, len(out.Roles))
		for _, r := range out.Roles {
			names = append(names, r.Name)
		}
		require.Contains(t, names, "ar-target")
	}

	// Requester creates an access request.
	var requestID string
	{
		ep := requester.clt.Endpoint("webapi", "sites", clusterName, "accessrequests")
		payload := map[string]any{
			"roles":         []string{"ar-target"},
			"requestReason": "testing end-to-end",
		}
		resp, err := requester.clt.PostJSON(ctx, ep, payload)
		require.NoError(t, err)
		var out ui.AccessRequest
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		require.Equal(t, "PENDING", out.State)
		require.Equal(t, []string{"ar-target"}, out.Roles)
		requestID = out.ID
		require.NotEmpty(t, requestID)
	}

	// Fetch single by ID.
	{
		ep := requester.clt.Endpoint("webapi", "sites", clusterName, "accessrequests", requestID)
		resp, err := requester.clt.Get(ctx, ep, url.Values{})
		require.NoError(t, err)
		var out ui.AccessRequest
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		require.Equal(t, requestID, out.ID)
	}

	// Reviewer fetches suggested access lists and sees the covering list.
	{
		ep := reviewer.clt.Endpoint("webapi", "sites", clusterName, "accessrequests", requestID, "suggested-access-lists")
		resp, err := reviewer.clt.Get(ctx, ep, url.Values{})
		require.NoError(t, err)
		var out struct {
			AccessLists []struct {
				Name string `json:"name"`
			} `json:"accessLists"`
		}
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		names := make([]string, 0, len(out.AccessLists))
		for _, l := range out.AccessLists {
			names = append(names, l.Name)
		}
		require.Contains(t, names, "cover-target", "expected cover-target access list in promotions")
	}

	// Reviewer approves.
	{
		ep := reviewer.clt.Endpoint("webapi", "sites", clusterName, "accessrequests", requestID, "review")
		payload := map[string]any{
			"state":  "APPROVED",
			"reason": "LGTM",
		}
		resp, err := reviewer.clt.PostJSON(ctx, ep, payload)
		require.NoError(t, err)
		var out ui.AccessRequest
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		require.Equal(t, "APPROVED", out.State)
	}

	// Requester deletes the request.
	{
		ep := requester.clt.Endpoint("webapi", "sites", clusterName, "accessrequests", requestID)
		_, err := requester.clt.Delete(ctx, ep)
		require.NoError(t, err)

		// List is empty again.
		listEp := requester.clt.Endpoint("webapi", "sites", clusterName, "accessrequests")
		resp, err := requester.clt.Get(ctx, listEp, url.Values{})
		require.NoError(t, err)
		var out struct {
			Items []ui.AccessRequest `json:"items"`
		}
		require.NoError(t, json.Unmarshal(resp.Bytes(), &out))
		require.Empty(t, out.Items)
	}
}

// unused variable guard; keeps context imports tidy if additional test pieces get added.
var _ = context.Background
