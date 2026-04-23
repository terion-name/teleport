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

package ui

import (
	"time"

	"github.com/gravitational/teleport/api/types"
)

// AccessRequest is the JSON shape consumed by makeAccessRequest() in
// web/packages/shared/services/accessRequests/makeAccessRequest.ts.
type AccessRequest struct {
	ID                      string                  `json:"id"`
	State                   string                  `json:"state"`
	User                    string                  `json:"user"`
	Expires                 time.Time               `json:"expires"`
	Created                 time.Time               `json:"created"`
	MaxDuration             *time.Time              `json:"maxDuration,omitempty"`
	RequestTTL              *time.Time              `json:"requestTTL,omitempty"`
	SessionTTL              *time.Time              `json:"sessionTTL,omitempty"`
	Roles                   []string                `json:"roles"`
	RequestReason           string                  `json:"requestReason"`
	ResolveReason           string                  `json:"resolveReason"`
	Reviews                 []AccessRequestReview   `json:"reviews"`
	SuggestedReviewers      []string                `json:"suggestedReviewers"`
	ThresholdNames          []string                `json:"thresholdNames"`
	Resources               []AccessRequestResource `json:"resources"`
	PromotedAccessListTitle string                  `json:"promotedAccessListTitle,omitempty"`
	AssumeStartTime         *time.Time              `json:"assumeStartTime,omitempty"`
	ReasonMode              string                  `json:"reasonMode"`
	ReasonPrompts           []string                `json:"reasonPrompts"`
	// RequestKind mirrors the AccessRequestKind enum on the proto (0=Undefined, 1=ShortTerm, 2=LongTerm).
	RequestKind int32 `json:"requestKind"`
}

// AccessRequestReview is one reviewer's decision on a request.
type AccessRequestReview struct {
	Author                  string     `json:"author"`
	State                   string     `json:"state"`
	Reason                  string     `json:"reason"`
	Roles                   []string   `json:"roles"`
	Created                 time.Time  `json:"created"`
	PromotedAccessListTitle string     `json:"promotedAccessListTitle,omitempty"`
	AssumeStartTime         *time.Time `json:"assumeStartTime,omitempty"`
}

// AccessRequestResource is a requested resource.
type AccessRequestResource struct {
	ID AccessRequestResourceID `json:"id"`
}

// AccessRequestResourceID uniquely identifies a requested resource.
type AccessRequestResourceID struct {
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	ClusterName     string `json:"clusterName"`
	SubResourceName string `json:"subResourceName,omitempty"`
}

// MakeAccessRequest converts a types.AccessRequest into the JSON shape the web UI expects.
func MakeAccessRequest(req types.AccessRequest) AccessRequest {
	out := AccessRequest{
		ID:                      req.GetName(),
		State:                   req.GetState().String(),
		User:                    req.GetUser(),
		Expires:                 req.Expiry(),
		Created:                 req.GetCreationTime(),
		Roles:                   append([]string{}, req.GetRoles()...),
		RequestReason:           req.GetRequestReason(),
		ResolveReason:           req.GetResolveReason(),
		Reviews:                 makeAccessRequestReviews(req.GetReviews()),
		SuggestedReviewers:      append([]string{}, req.GetSuggestedReviewers()...),
		ThresholdNames:          collectThresholdNames(req.GetThresholds()),
		Resources:               makeAccessRequestResources(req.GetRequestedResourceIDs()),
		PromotedAccessListTitle: req.GetPromotedAccessListTitle(),
		RequestKind:             int32(req.GetRequestKind()),
	}

	if md := req.GetMaxDuration(); !md.IsZero() {
		out.MaxDuration = &md
	}
	if st := req.GetSessionTLL(); !st.IsZero() {
		out.SessionTTL = &st
	}
	if rt := req.Expiry(); !rt.IsZero() {
		// requestTTL mirrors Expiry when no distinct field is present.
		out.RequestTTL = &rt
	}
	if ast := req.GetAssumeStartTime(); ast != nil {
		out.AssumeStartTime = ast
	}
	if enrichment := req.GetDryRunEnrichment(); enrichment != nil {
		out.ReasonMode = string(enrichment.ReasonMode)
		out.ReasonPrompts = append([]string{}, enrichment.ReasonPrompts...)
	}
	return out
}

// MakeAccessRequests converts a slice of access requests to their UI representations.
func MakeAccessRequests(reqs []types.AccessRequest) []AccessRequest {
	out := make([]AccessRequest, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, MakeAccessRequest(r))
	}
	return out
}

func makeAccessRequestReviews(reviews []types.AccessReview) []AccessRequestReview {
	out := make([]AccessRequestReview, 0, len(reviews))
	for _, rv := range reviews {
		ar := AccessRequestReview{
			Author:  rv.Author,
			State:   rv.ProposedState.String(),
			Reason:  rv.Reason,
			Roles:   append([]string{}, rv.Roles...),
			Created: rv.Created,
		}
		if rv.AccessList != nil {
			ar.PromotedAccessListTitle = rv.AccessList.Title
		}
		if rv.AssumeStartTime != nil {
			ar.AssumeStartTime = rv.AssumeStartTime
		}
		out = append(out, ar)
	}
	return out
}

func collectThresholdNames(thresholds []types.AccessReviewThreshold) []string {
	names := make([]string, 0, len(thresholds))
	for _, t := range thresholds {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names
}

func makeAccessRequestResources(ids []types.ResourceID) []AccessRequestResource {
	out := make([]AccessRequestResource, 0, len(ids))
	for _, id := range ids {
		out = append(out, AccessRequestResource{
			ID: AccessRequestResourceID{
				Kind:            id.Kind,
				Name:            id.Name,
				ClusterName:     id.ClusterName,
				SubResourceName: id.SubResourceName,
			},
		})
	}
	return out
}
