/**
 * Teleport
 * Copyright (C) 2026 Gravitational, Inc.
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

import {
  AccessRequest,
  makeAccessRequest,
} from 'shared/services/accessRequests';

import cfg from 'teleport/config';
import api from 'teleport/services/api';

import {
  CreateAccessRequestPayload,
  RequestableResourceRolesPayload,
  RequestableResourceRolesResponse,
  RequestableRolesResponse,
  SubmitReviewPayload,
  SuggestedAccessList,
} from './types';

/**
 * AccessRequestService wraps the OSS web API for access requests.
 * Each method maps 1:1 to a handler in lib/web/access_request.go.
 */
export class AccessRequestService {
  async fetchAccessRequests(
    clusterId: string,
    filter?: {
      state?: 'PENDING' | 'APPROVED' | 'DENIED' | 'PROMOTED';
      scope?: 'MY_REQUESTS' | 'NEEDS_REVIEW' | 'REVIEWED';
      user?: string;
    }
  ): Promise<AccessRequest[]> {
    const base = cfg.getAccessRequestsListUrl(clusterId);
    const query = new URLSearchParams();
    if (filter?.state) query.set('state', filter.state);
    if (filter?.scope) query.set('scope', filter.scope);
    if (filter?.user) query.set('user', filter.user);
    const url = query.toString() ? `${base}?${query.toString()}` : base;
    const json = await api.get(url);
    return (json.items || []).map(makeAccessRequest);
  }

  async fetchAccessRequest(
    clusterId: string,
    requestId: string
  ): Promise<AccessRequest> {
    const json = await api.get(
      cfg.getAccessRequestByIdUrl(clusterId, requestId)
    );
    return makeAccessRequest(json);
  }

  async createAccessRequest(
    clusterId: string,
    payload: CreateAccessRequestPayload
  ): Promise<AccessRequest> {
    const json = await api.post(
      cfg.getAccessRequestsListUrl(clusterId),
      payload
    );
    return makeAccessRequest(json);
  }

  async deleteAccessRequest(
    clusterId: string,
    requestId: string
  ): Promise<void> {
    await api.delete(cfg.getAccessRequestByIdUrl(clusterId, requestId));
  }

  async submitAccessReview(
    clusterId: string,
    requestId: string,
    review: SubmitReviewPayload
  ): Promise<AccessRequest> {
    const json = await api.post(
      cfg.getAccessRequestReviewUrl(clusterId, requestId),
      review
    );
    return makeAccessRequest(json);
  }

  async fetchRequestableRoles(
    clusterId: string
  ): Promise<RequestableRolesResponse> {
    const json = await api.get(cfg.getRequestableRolesUrl(clusterId));
    return {
      roles: json.roles || [],
      nextPageToken: json.nextPageToken,
    };
  }

  async fetchRequestableResourceRoles(
    clusterId: string,
    payload: RequestableResourceRolesPayload
  ): Promise<RequestableResourceRolesResponse> {
    const json = await api.post(
      cfg.getRequestableResourceRolesUrl(clusterId),
      payload
    );
    return {
      applicableRolesForResources: json.applicableRolesForResources || [],
      requestableRoles: json.requestableRoles || [],
    };
  }

  async fetchSuggestedAccessLists(
    clusterId: string,
    requestId: string
  ): Promise<SuggestedAccessList[]> {
    const json = await api.get(
      cfg.getAccessRequestSuggestedAccessListsUrl(clusterId, requestId)
    );
    return (json.accessLists || []) as SuggestedAccessList[];
  }
}

const accessRequestService = new AccessRequestService();

export default accessRequestService;
