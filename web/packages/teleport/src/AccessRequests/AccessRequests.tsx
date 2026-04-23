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

import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import styled from 'styled-components';

import {
  Alert,
  Box,
  ButtonPrimary,
  ButtonSecondary,
  Flex,
  H2,
  Indicator,
  Label as LabelUI,
  Text,
} from 'design';
import { ArrowLeft, Refresh } from 'design/Icon';
import { AccessRequest, RequestState } from 'shared/services/accessRequests';

import {
  FeatureBox,
  FeatureHeader,
  FeatureHeaderTitle,
} from 'teleport/components/Layout';
import cfg from 'teleport/config';
import useTeleport from 'teleport/useTeleport';

import { CreateAccessRequest } from './CreateAccessRequest';
import { RequestDetail } from './RequestDetail';
import { RequestsTable } from './RequestsTable';

type View = 'list' | 'create' | 'detail';
type Scope = 'all' | 'mine' | 'review';
type StateFilter =
  | 'all'
  | 'PENDING'
  | 'APPROVED'
  | 'DENIED'
  | 'PROMOTED';

/**
 * AccessRequests is the OSS container for the Access Requests feature.
 * Routes:
 *   /web/accessrequest          → list (optionally create)
 *   /web/requests/:requestId    → single-request view
 */
export function AccessRequests() {
  const ctx = useTeleport();
  const navigate = useNavigate();
  const { requestId } = useParams<{ requestId?: string }>();
  const clusterId = ctx.storeUser.getClusterId();

  const initialView: View = requestId ? 'detail' : 'list';
  const [view, setView] = useState<View>(initialView);
  const [requests, setRequests] = useState<AccessRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [scope, setScope] = useState<Scope>('all');
  const [stateFilter, setStateFilter] = useState<StateFilter>('all');
  const [userFilter, setUserFilter] = useState('');

  useEffect(() => {
    if (requestId) {
      setView('detail');
    }
  }, [requestId]);

  async function refreshList() {
    setLoading(true);
    setError(null);
    try {
      const items = await ctx.accessRequestService.fetchAccessRequests(
        clusterId
      );
      setRequests(items);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refreshList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId]);

  const currentUser = ctx.storeUser.getUsername();

  // Pending reviews for the current user = requests authored by someone else
  // that are still in PENDING state. (Server-side filtering already limits
  // the list to requests this user can review or owns; anything PENDING
  // that isn't mine therefore needs my review.)
  const pendingReviewCount = useMemo(() => {
    return requests.filter(
      r => r.state === 'PENDING' && r.user !== currentUser
    ).length;
  }, [requests, currentUser]);

  // Update the document title so it is visible in the browser tab.
  useEffect(() => {
    if (pendingReviewCount > 0) {
      const base = document.title.replace(/^\(\d+\)\s/, '');
      document.title = `(${pendingReviewCount}) ${base}`;
    } else {
      document.title = document.title.replace(/^\(\d+\)\s/, '');
    }
  }, [pendingReviewCount]);

  const filteredRequests = useMemo(() => {
    return requests.filter(r => {
      if (scope === 'mine' && r.user !== currentUser) return false;
      if (scope === 'review' && r.user === currentUser) return false;
      if (stateFilter !== 'all' && r.state !== stateFilter) return false;
      if (userFilter.trim() && !r.user.includes(userFilter.trim())) return false;
      return true;
    });
  }, [requests, scope, stateFilter, userFilter, currentUser]);

  function openDetail(id: string) {
    navigate(cfg.getAccessRequestRoute(id));
    setView('detail');
  }

  function backToList() {
    navigate(cfg.routes.accessRequest);
    setView('list');
    refreshList();
  }

  function startCreate() {
    setView('create');
  }

  if (view === 'create') {
    return (
      <FeatureBox>
        <FeatureHeader>
          <BackButton onClick={backToList} />
          <FeatureHeaderTitle>New Access Request</FeatureHeaderTitle>
        </FeatureHeader>
        <CreateAccessRequest
          clusterId={clusterId}
          onCreated={() => {
            backToList();
          }}
          onCancel={backToList}
        />
      </FeatureBox>
    );
  }

  if (view === 'detail' && requestId) {
    return (
      <FeatureBox>
        <FeatureHeader>
          <BackButton onClick={backToList} />
          <FeatureHeaderTitle>Access Request</FeatureHeaderTitle>
        </FeatureHeader>
        <RequestDetail
          clusterId={clusterId}
          requestId={requestId}
          currentUser={ctx.storeUser.getUsername()}
          onChanged={() => {
            refreshList();
          }}
        />
      </FeatureBox>
    );
  }

  return (
    <FeatureBox>
      <FeatureHeader>
        <FeatureHeaderTitle>Access Requests</FeatureHeaderTitle>
        <Flex gap={2} ml="auto" alignItems="center">
          {pendingReviewCount > 0 && (
            <ReviewBadge
              onClick={() => {
                setScope('review');
                setStateFilter('PENDING');
              }}
              title="Click to filter: pending your review"
            >
              {pendingReviewCount} awaiting your review
            </ReviewBadge>
          )}
          <ButtonSecondary onClick={refreshList} disabled={loading}>
            <Refresh size="small" mr={1} />
            Refresh
          </ButtonSecondary>
          <ButtonPrimary onClick={startCreate}>New Request</ButtonPrimary>
        </Flex>
      </FeatureHeader>

      <FiltersRow>
        <ScopeTabs>
          <ScopeTab
            data-active={scope === 'all'}
            onClick={() => setScope('all')}
          >
            All
          </ScopeTab>
          <ScopeTab
            data-active={scope === 'mine'}
            onClick={() => setScope('mine')}
          >
            Mine
          </ScopeTab>
          <ScopeTab
            data-active={scope === 'review'}
            onClick={() => setScope('review')}
          >
            To review
          </ScopeTab>
        </ScopeTabs>
        <StateSelect
          value={stateFilter}
          onChange={e => setStateFilter(e.target.value as StateFilter)}
        >
          <option value="all">All statuses</option>
          <option value="PENDING">Pending</option>
          <option value="APPROVED">Approved</option>
          <option value="DENIED">Denied</option>
          <option value="PROMOTED">Promoted</option>
        </StateSelect>
        <UserInput
          placeholder="Filter by user…"
          value={userFilter}
          onChange={e => setUserFilter(e.target.value)}
        />
      </FiltersRow>

      {error && <Alert kind="danger">{error}</Alert>}
      {loading ? (
        <Box textAlign="center" m={10}>
          <Indicator />
        </Box>
      ) : requests.length === 0 ? (
        <EmptyState>
          <H2 mb={2}>No access requests yet</H2>
          <Text mb={4}>
            Click "New Request" to ask for temporary elevated access.
          </Text>
          <ButtonPrimary onClick={startCreate}>New Request</ButtonPrimary>
        </EmptyState>
      ) : filteredRequests.length === 0 ? (
        <EmptyState>
          <Text color="text.slightlyMuted">
            No requests match the current filters.
          </Text>
        </EmptyState>
      ) : (
        <RequestsTable
          requests={filteredRequests}
          currentUser={currentUser}
          onView={openDetail}
        />
      )}
    </FeatureBox>
  );
}

function BackButton({ onClick }: { onClick: () => void }) {
  return (
    <ButtonSecondary onClick={onClick} mr={3}>
      <ArrowLeft size="medium" mr={1} />
      Back
    </ButtonSecondary>
  );
}

const EmptyState = styled(Box)`
  text-align: center;
  padding: 64px 16px;
`;

const FiltersRow = styled(Flex)`
  gap: 12px;
  margin-bottom: 16px;
  align-items: center;
  flex-wrap: wrap;
`;

const ScopeTabs = styled(Flex)`
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
  overflow: hidden;
`;

const ScopeTab = styled.button`
  background: transparent;
  border: none;
  color: ${p => p.theme.colors.text.main};
  padding: 6px 14px;
  cursor: pointer;
  font-size: 13px;
  border-right: 1px solid ${p => p.theme.colors.spotBackground[1]};

  &:last-of-type {
    border-right: none;
  }

  &[data-active='true'] {
    background: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
`;

const StateSelect = styled.select`
  background: ${p => p.theme.colors.levels.surface};
  color: ${p => p.theme.colors.text.main};
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
  padding: 6px 10px;
  font-size: 13px;
`;

const UserInput = styled.input`
  background: ${p => p.theme.colors.levels.surface};
  color: ${p => p.theme.colors.text.main};
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
  padding: 6px 10px;
  font-size: 13px;
  min-width: 180px;
`;

const ReviewBadge = styled.button`
  background: ${p => p.theme.colors.warning.main};
  color: ${p => p.theme.colors.text.primaryInverse};
  border: none;
  padding: 6px 12px;
  border-radius: 999px;
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;

  &:hover {
    filter: brightness(1.1);
  }
`;

// Exposed for completeness; the primary consumer wraps AccessRequests in a router.
export type AccessRequestState = RequestState;
export type AccessRequestItem = AccessRequest;
export { LabelUI };
