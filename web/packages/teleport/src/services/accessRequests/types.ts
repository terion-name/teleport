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

import type { RequestState } from 'shared/services/accessRequests';

/**
 * CreateAccessRequestPayload is the request body for POST /webapi/sites/:site/accessrequests.
 */
export interface CreateAccessRequestPayload {
  /** roles is the list of roles being requested. */
  roles?: string[];
  /** resources is the list of resources being requested. */
  resources?: AccessRequestResourceIdPayload[];
  /** requestReason is the reason supplied by the requester. */
  requestReason?: string;
  /** suggestedReviewers is an optional list of suggested reviewers. */
  suggestedReviewers?: string[];
  /** maxDuration (ISO8601). Optional override. */
  maxDuration?: string;
  /** requestTTL (ISO8601). Optional override. */
  requestTTL?: string;
  /** assumeStartTime (ISO8601). */
  assumeStartTime?: string;
  /** When true, validate without persisting. Server returns a fully populated request. */
  dryRun?: boolean;
  /** requestKind matches the AccessRequestKind enum on the proto. */
  requestKind?: number;
}

/** AccessRequestResourceIdPayload is the JSON form of a requested resource identifier. */
export interface AccessRequestResourceIdPayload {
  kind: string;
  name: string;
  clusterName: string;
  subResourceName?: string;
}

/** SubmitReviewPayload is the body for POST /review. */
export interface SubmitReviewPayload {
  state: RequestState;
  reason?: string;
  roles?: string[];
  assumeStartTime?: string;
  /** Non-empty only when promoting via an access list. */
  promotedAccessListName?: string;
}

/** RequestableRole is a single requestable role entry. */
export interface RequestableRole {
  name: string;
  description: string;
}

/** RequestableRolesResponse is the GET /requestableroles response. */
export interface RequestableRolesResponse {
  roles: RequestableRole[];
  nextPageToken?: string;
}

/** RequestableResourceRolesPayload is the POST /requestableroles/resources body. */
export interface RequestableResourceRolesPayload {
  resourceIds: AccessRequestResourceIdPayload[];
  login?: string;
  filterRequestableRolesByResource?: boolean;
}

/** RequestableResourceRolesResponse is the /requestableroles/resources response. */
export interface RequestableResourceRolesResponse {
  applicableRolesForResources: string[];
  requestableRoles: string[];
}

/** SuggestedAccessList is one entry returned by /suggested-access-lists. */
export interface SuggestedAccessList {
  name: string;
}
