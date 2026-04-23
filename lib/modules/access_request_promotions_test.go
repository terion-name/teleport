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

package modules

import (
	"context"
	"iter"
	"testing"

	"github.com/gravitational/trace"
	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport/api/client/proto"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/api/types/accesslist"
	"github.com/gravitational/teleport/api/types/header"
)

type fakeAccessResourcesGetter struct {
	lists   []*accesslist.AccessList
	members map[string]map[string]bool // list name -> user name -> true
}

func (f *fakeAccessResourcesGetter) GetAccessLists(ctx context.Context) ([]*accesslist.AccessList, error) {
	return f.lists, nil
}

func (f *fakeAccessResourcesGetter) ListAccessLists(ctx context.Context, _ int, _ string) ([]*accesslist.AccessList, string, error) {
	return f.lists, "", nil
}

func (f *fakeAccessResourcesGetter) GetAccessList(ctx context.Context, name string) (*accesslist.AccessList, error) {
	for _, l := range f.lists {
		if l.GetName() == name {
			return l, nil
		}
	}
	return nil, trace.NotFound("access list %q not found", name)
}

func (f *fakeAccessResourcesGetter) GetAccessListMember(ctx context.Context, listName, user string) (*accesslist.AccessListMember, error) {
	if f.members[listName][user] {
		return &accesslist.AccessListMember{}, nil
	}
	return nil, trace.NotFound("member %q not in list %q", user, listName)
}

func (f *fakeAccessResourcesGetter) ListAccessListMembers(ctx context.Context, _ string, _ int, _ string) ([]*accesslist.AccessListMember, string, error) {
	return nil, "", nil
}

func (f *fakeAccessResourcesGetter) GetAccessListOwners(ctx context.Context, _ string) ([]*accesslist.Owner, error) {
	return nil, nil
}

func (f *fakeAccessResourcesGetter) ListResources(ctx context.Context, _ proto.ListResourcesRequest) (*types.ListResourcesResponse, error) {
	return &types.ListResourcesResponse{}, nil
}

func (f *fakeAccessResourcesGetter) GetUser(ctx context.Context, _ string, _ bool) (types.User, error) {
	return nil, trace.NotFound("not implemented")
}

func (f *fakeAccessResourcesGetter) GetRole(ctx context.Context, _ string) (types.Role, error) {
	return nil, trace.NotFound("not implemented")
}

func (f *fakeAccessResourcesGetter) GetLock(ctx context.Context, _ string) (types.Lock, error) {
	return nil, trace.NotFound("not implemented")
}

func (f *fakeAccessResourcesGetter) GetLocks(ctx context.Context, _ bool, _ ...types.LockTarget) ([]types.Lock, error) {
	return nil, nil
}

func (f *fakeAccessResourcesGetter) ListLocks(ctx context.Context, _ int, _ string, _ *types.LockFilter) ([]types.Lock, string, error) {
	return nil, "", nil
}

func (f *fakeAccessResourcesGetter) RangeLocks(ctx context.Context, _, _ string, _ *types.LockFilter) iter.Seq2[types.Lock, error] {
	return func(yield func(types.Lock, error) bool) {}
}

func newTestAccessList(t *testing.T, name string, grantedRoles []string) *accesslist.AccessList {
	t.Helper()
	list, err := accesslist.NewAccessList(
		header.Metadata{Name: name},
		accesslist.Spec{
			Title: name + "-title",
			Type:  accesslist.Static,
			Grants: accesslist.Grants{
				Roles: grantedRoles,
			},
		},
	)
	require.NoError(t, err)
	return list
}

func TestGenerateAccessRequestPromotions(t *testing.T) {
	ctx := context.Background()

	covers := newTestAccessList(t, "covers", []string{"access", "kube-access"})
	partial := newTestAccessList(t, "partial", []string{"access"})
	superset := newTestAccessList(t, "superset", []string{"access", "kube-access", "auditor"})
	memberAlready := newTestAccessList(t, "already-member", []string{"access", "kube-access"})

	clt := &fakeAccessResourcesGetter{
		lists: []*accesslist.AccessList{covers, partial, superset, memberAlready},
		members: map[string]map[string]bool{
			"already-member": {"alice": true},
		},
	}

	tests := []struct {
		name             string
		user             string
		roles            []string
		expectPromotions []string
	}{
		{
			name:             "covers and superset, excluding lists user is already a member of",
			user:             "alice",
			roles:            []string{"access", "kube-access"},
			expectPromotions: []string{"covers", "superset"},
		},
		{
			name:             "all matching lists when user is not a member of any",
			user:             "bob",
			roles:            []string{"access", "kube-access"},
			expectPromotions: []string{"covers", "superset", "already-member"},
		},
		{
			name:             "no promotions for unmatched role",
			user:             "alice",
			roles:            []string{"nonexistent"},
			expectPromotions: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := types.NewAccessRequest("req-"+tc.user, tc.user, tc.roles...)
			require.NoError(t, err)

			promotions, err := generateAccessRequestPromotions(ctx, clt, req)
			require.NoError(t, err)
			require.NotNil(t, promotions)

			got := make([]string, 0, len(promotions.Promotions))
			for _, p := range promotions.Promotions {
				got = append(got, p.AccessListName)
			}
			require.ElementsMatch(t, tc.expectPromotions, got)
		})
	}
}

func TestGenerateAccessRequestPromotions_ResourceOnlyShortCircuit(t *testing.T) {
	ctx := context.Background()

	clt := &fakeAccessResourcesGetter{
		lists: []*accesslist.AccessList{newTestAccessList(t, "covers", []string{"access"})},
	}

	// Build a request with roles, then drop them to simulate the resource-only path.
	req, err := types.NewAccessRequest("req-1", "alice", "access")
	require.NoError(t, err)
	req.SetRoles(nil)

	promotions, err := generateAccessRequestPromotions(ctx, clt, req)
	require.NoError(t, err)
	require.NotNil(t, promotions)
	require.Empty(t, promotions.Promotions)
}

func TestGrantsCoverRoles(t *testing.T) {
	tests := []struct {
		name      string
		granted   []string
		requested []string
		want      bool
	}{
		{"exact match", []string{"a", "b"}, []string{"a", "b"}, true},
		{"granted superset", []string{"a", "b", "c"}, []string{"a", "b"}, true},
		{"requested role missing", []string{"a", "c"}, []string{"a", "b"}, false},
		{"granted empty", []string{}, []string{"a"}, false},
		{"requested empty", []string{"a"}, []string{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reqSet := make(map[string]struct{}, len(tc.requested))
			for _, r := range tc.requested {
				reqSet[r] = struct{}{}
			}
			require.Equal(t, tc.want, grantsCoverRoles(tc.granted, reqSet))
		})
	}
}
