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

	"github.com/gravitational/trace"

	"github.com/gravitational/teleport/api/types"
)

// GenerateOSSAccessRequestPromotions is the exported entry point for the OSS
// promotion algorithm. It's exposed so tests can wire it into the mock modules.
func GenerateOSSAccessRequestPromotions(ctx context.Context, clt AccessResourcesGetter, req types.AccessRequest) (*types.AccessRequestAllowedPromotions, error) {
	return generateAccessRequestPromotions(ctx, clt, req)
}

// generateAccessRequestPromotions is the OSS promotion algorithm.
//
// For each access list, it checks whether that list's member-grants contain every role
// the request asks for. If yes, the list is suggested as a potential promotion — granting
// the user membership would give them the requested access.
//
// The user already being a member of an access list yields a trivial promotion: they'd
// gain nothing by being re-added. Those lists are filtered out.
//
// Resource-only requests are not covered (request has no roles); they receive no promotions.
func generateAccessRequestPromotions(ctx context.Context, clt AccessResourcesGetter, req types.AccessRequest) (*types.AccessRequestAllowedPromotions, error) {
	if len(req.GetRoles()) == 0 {
		return types.NewAccessRequestAllowedPromotions(nil), nil
	}

	requested := make(map[string]struct{}, len(req.GetRoles()))
	for _, r := range req.GetRoles() {
		requested[r] = struct{}{}
	}

	lists, err := clt.GetAccessLists(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	promotions := make([]*types.AccessRequestAllowedPromotion, 0)
	for _, list := range lists {
		// Skip lists whose grants don't cover the requested roles.
		if !grantsCoverRoles(list.Spec.Grants.Roles, requested) {
			continue
		}

		// Skip lists the user is already a direct member of.
		isMember, err := userIsDirectMember(ctx, clt, list.GetName(), req.GetUser())
		if err != nil {
			return nil, trace.Wrap(err)
		}
		if isMember {
			continue
		}

		promotions = append(promotions, &types.AccessRequestAllowedPromotion{
			AccessListName: list.GetName(),
		})
	}

	return types.NewAccessRequestAllowedPromotions(promotions), nil
}

// grantsCoverRoles returns true iff every role in `requested` appears in `granted`.
func grantsCoverRoles(granted []string, requested map[string]struct{}) bool {
	if len(granted) < len(requested) {
		return false
	}
	grantedSet := make(map[string]struct{}, len(granted))
	for _, r := range granted {
		grantedSet[r] = struct{}{}
	}
	for r := range requested {
		if _, ok := grantedSet[r]; !ok {
			return false
		}
	}
	return true
}

// userIsDirectMember reports whether the named user is a direct member (kind=user) of the list.
// Nested-list memberships are not considered — a user promoted via this list will receive
// direct membership, so prior nested membership shouldn't hide the list from suggestions.
func userIsDirectMember(ctx context.Context, clt AccessResourcesGetter, listName, userName string) (bool, error) {
	member, err := clt.GetAccessListMember(ctx, listName, userName)
	if err != nil {
		if trace.IsNotFound(err) {
			return false, nil
		}
		return false, trace.Wrap(err)
	}
	return member != nil, nil
}
