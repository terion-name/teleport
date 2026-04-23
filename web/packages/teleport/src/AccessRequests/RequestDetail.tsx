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
import styled from 'styled-components';

import {
  Alert,
  Box,
  ButtonPrimary,
  ButtonSecondary,
  ButtonWarning,
  Flex,
  H2,
  H3,
  Indicator,
  Label as Pill,
  Text,
} from 'design';
import FieldInput from 'shared/components/FieldInput';
import Validation from 'shared/components/Validation';
import {
  AccessRequest,
  RequestState,
} from 'shared/services/accessRequests';

import session from 'teleport/services/websession';
import useTeleport from 'teleport/useTeleport';

type Props = {
  clusterId: string;
  requestId: string;
  currentUser: string;
  onChanged: () => void;
};

type SuggestedList = { name: string };

/**
 * RequestDetail renders a single access request and lets reviewers approve,
 * deny, or promote it. Deletes are also allowed when the current user owns
 * the request or has delete access.
 */
export function RequestDetail({
  clusterId,
  requestId,
  currentUser,
  onChanged,
}: Props) {
  const ctx = useTeleport();

  const [request, setRequest] = useState<AccessRequest | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [suggestions, setSuggestions] = useState<SuggestedList[]>([]);
  const [reviewReason, setReviewReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [reviewRoles, setReviewRoles] = useState<string[] | null>(null);

  // Tick every second so countdowns stay fresh without re-fetching.
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => setTick(t => t + 1), 1000);
    return () => window.clearInterval(id);
  }, []);

  async function refresh() {
    setLoading(true);
    setError(null);
    try {
      const [req, sug] = await Promise.all([
        ctx.accessRequestService.fetchAccessRequest(clusterId, requestId),
        ctx.accessRequestService
          .fetchSuggestedAccessLists(clusterId, requestId)
          .catch(() => [] as SuggestedList[]),
      ]);
      setRequest(req);
      setSuggestions(sug);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId, requestId]);

  async function review(
    state: RequestState,
    promotedAccessListName?: string
  ) {
    setSubmitting(true);
    setActionError(null);
    try {
      await ctx.accessRequestService.submitAccessReview(
        clusterId,
        requestId,
        {
          state,
          reason: reviewReason,
          promotedAccessListName,
          // Only send the subset when the reviewer has narrowed it.
          roles:
            reviewRoles && reviewRoles.length > 0 ? reviewRoles : undefined,
        }
      );
      setReviewReason('');
      setReviewRoles(null);
      await refresh();
      onChanged();
    } catch (e) {
      setActionError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  async function assumeRoles() {
    setSubmitting(true);
    setActionError(null);
    try {
      await session.renewSession({ requestId });
      // Reload the page so every downstream component re-reads the new roles.
      window.location.reload();
    } catch (e) {
      setActionError((e as Error).message);
      setSubmitting(false);
    }
  }

  async function switchback() {
    setSubmitting(true);
    setActionError(null);
    try {
      await session.renewSession({ switchback: true });
      window.location.reload();
    } catch (e) {
      setActionError((e as Error).message);
      setSubmitting(false);
    }
  }

  async function destroy() {
    if (!window.confirm('Delete this access request?')) return;
    setSubmitting(true);
    setActionError(null);
    try {
      await ctx.accessRequestService.deleteAccessRequest(clusterId, requestId);
      onChanged();
      // Parent will redirect.
      window.history.back();
    } catch (e) {
      setActionError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  if (loading) {
    return (
      <Box textAlign="center" m={10}>
        <Indicator />
      </Box>
    );
  }
  if (error) return <Alert kind="danger">{error}</Alert>;
  if (!request) return <Alert kind="warning">Request not found.</Alert>;

  const isMine = request.user === currentUser;
  const pending = request.state === 'PENDING';
  const canReview = pending && !isMine;
  const approved = request.state === 'APPROVED';
  const assumedRequestId = ctx.storeUser.getAccessRequestId();
  const isAssumed = assumedRequestId === request.id;
  const canAssume = approved && isMine && !isAssumed;

  return (
    <Validation>
      <Box>
        {actionError && <Alert kind="danger">{actionError}</Alert>}

      <Section>
        <Flex alignItems="baseline" gap={3} flexWrap="wrap">
          <H2>{request.user}</H2>
          <StateBadge state={request.state}>{request.state}</StateBadge>
          <Text color="text.slightlyMuted">
            {request.createdDuration || ''}
          </Text>
        </Flex>
        <Text
          mt={1}
          color="text.slightlyMuted"
          typography="body2"
          style={{ fontFamily: 'monospace' }}
        >
          {request.id}
        </Text>
      </Section>

      <Section>
        <Flex gap={4} flexWrap="wrap">
          <Metric
            label="Request expires in"
            value={liveCountdown(request.expires)}
          />
          <Metric
            label="Elevated access expires in"
            value={liveCountdown(request.sessionTTL)}
          />
          <Metric
            label="Max duration"
            value={request.maxDurationText || '—'}
          />
        </Flex>
      </Section>

      <Section>
        <H3 mb={2}>Requested roles</H3>
        {request.roles.length === 0 ? (
          <Text color="text.slightlyMuted">No roles</Text>
        ) : (
          <Flex gap={1} flexWrap="wrap">
            {request.roles.map(r => (
              <Pill key={r} kind="secondary">
                {r}
              </Pill>
            ))}
          </Flex>
        )}
      </Section>

      {request.resources.length > 0 && (
        <Section>
          <H3 mb={2}>Requested resources</H3>
          <ResourcesList>
            {request.resources.map((res, i) => (
              <li key={i}>
                <code>
                  {res.id.kind}/{res.id.name}
                </code>
                {res.id.clusterName && (
                  <Text
                    as="span"
                    color="text.slightlyMuted"
                    typography="body3"
                    ml={1}
                  >
                    @{res.id.clusterName}
                  </Text>
                )}
              </li>
            ))}
          </ResourcesList>
        </Section>
      )}

      {request.requestReason && (
        <Section>
          <H3 mb={2}>Reason</H3>
          <Text>{request.requestReason}</Text>
        </Section>
      )}

      {request.resolveReason && (
        <Section>
          <H3 mb={2}>Resolve reason</H3>
          <Text>{request.resolveReason}</Text>
        </Section>
      )}

      {request.reviewers.length > 0 && (
        <Section>
          <H3 mb={2}>Reviewers</H3>
          <Flex gap={2} flexWrap="wrap">
            {request.reviewers.map(rev => (
              <ReviewerPill key={rev.name} data-state={rev.state}>
                {rev.name}
                <ReviewerState>{rev.state}</ReviewerState>
              </ReviewerPill>
            ))}
          </Flex>
        </Section>
      )}

      {request.reviews.length > 0 && (
        <Section>
          <H3 mb={2}>Reviews</H3>
          <ReviewsList>
            {request.reviews.map((rev, i) => (
              <li key={i}>
                <Flex alignItems="baseline" gap={2}>
                  <strong>{rev.author}</strong>
                  <StateBadge state={rev.state}>{rev.state}</StateBadge>
                  <Text color="text.slightlyMuted" typography="body3">
                    {rev.createdDuration}
                  </Text>
                </Flex>
                {rev.reason && <Text mt={1}>{rev.reason}</Text>}
                {rev.promotedAccessListTitle && (
                  <Text mt={1} color="text.slightlyMuted" typography="body3">
                    Promoted via access list:{' '}
                    {rev.promotedAccessListTitle}
                  </Text>
                )}
              </li>
            ))}
          </ReviewsList>
        </Section>
      )}

      {canReview && (
        <Section>
          <H3 mb={2}>Submit review</H3>

          {request.roles.length > 0 && (
            <Box mb={3}>
              <Text typography="body2" color="text.slightlyMuted" mb={1}>
                Approve a subset of roles (leave all selected to approve as
                requested):
              </Text>
              <Flex gap={1} flexWrap="wrap">
                {request.roles.map(r => {
                  const selected =
                    reviewRoles === null || reviewRoles.includes(r);
                  return (
                    <RoleChip
                      key={r}
                      data-selected={selected}
                      onClick={() => {
                        setReviewRoles(prev => {
                          // Initialize from all roles on first toggle.
                          const base = prev ?? [...request.roles];
                          return base.includes(r)
                            ? base.filter(x => x !== r)
                            : [...base, r];
                        });
                      }}
                    >
                      {r}
                    </RoleChip>
                  );
                })}
              </Flex>
            </Box>
          )}

          <FieldInput
            placeholder="Review reason (optional)"
            value={reviewReason}
            onChange={e => setReviewReason(e.target.value)}
          />
          <Flex gap={2} mt={2} flexWrap="wrap">
            <ButtonPrimary
              disabled={submitting}
              onClick={() => review('APPROVED')}
            >
              Approve
            </ButtonPrimary>
            <ButtonWarning
              disabled={submitting}
              onClick={() => review('DENIED')}
            >
              Deny
            </ButtonWarning>
          </Flex>

          {suggestions.length > 0 && (
            <Box mt={4}>
              <Text typography="body2" mb={2}>
                Suggested access lists that cover the requested roles:
              </Text>
              <Flex gap={2} flexWrap="wrap">
                {suggestions.map(sug => (
                  <ButtonSecondary
                    key={sug.name}
                    disabled={submitting}
                    onClick={() => review('PROMOTED', sug.name)}
                    title={`Promote ${request.user} via "${sug.name}" access list`}
                  >
                    Promote via {sug.name}
                  </ButtonSecondary>
                ))}
              </Flex>
            </Box>
          )}
        </Section>
      )}

      {canAssume && (
        <Section>
          <H3 mb={2}>Assume elevated roles</H3>
          <Text typography="body2" color="text.slightlyMuted" mb={2}>
            Re-issues your web session with the roles granted by this
            request. The elevated roles expire automatically when the
            request's session TTL runs out.
          </Text>
          <ButtonPrimary disabled={submitting} onClick={assumeRoles}>
            Assume roles
          </ButtonPrimary>
        </Section>
      )}

      {isAssumed && (
        <Section>
          <H3 mb={2}>Currently assumed</H3>
          <Text typography="body2" color="text.slightlyMuted" mb={2}>
            You are using this request's elevated roles. Switch back to
            drop them before the natural expiry.
          </Text>
          <ButtonSecondary disabled={submitting} onClick={switchback}>
            Switch back to base roles
          </ButtonSecondary>
        </Section>
      )}

      <Section>
        <Flex gap={2}>
          <ButtonSecondary disabled={submitting} onClick={destroy}>
            Delete request
          </ButtonSecondary>
        </Flex>
        </Section>
      </Box>
    </Validation>
  );
}

function StateBadge({
  state,
  children,
}: {
  state: RequestState;
  children: React.ReactNode;
}) {
  return <Badge data-state={state}>{children}</Badge>;
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Box>
      <Text typography="body3" color="text.slightlyMuted">
        {label}
      </Text>
      <Text typography="h4">{value}</Text>
    </Box>
  );
}

/** liveCountdown formats the time between now and `when` as HH:MM:SS, or
 * "expired" / "—" for invalid/past/null inputs. The parent component should
 * re-render periodically so the returned string stays fresh.
 */
function liveCountdown(when?: Date | null): string {
  if (!when) return '—';
  const ms = new Date(when).getTime() - Date.now();
  if (!Number.isFinite(ms)) return '—';
  if (ms <= 0) return 'expired';
  const s = Math.floor(ms / 1000);
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  if (hours >= 24) {
    const d = Math.floor(hours / 24);
    return `${d}d ${hours % 24}h ${minutes}m`;
  }
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
}

const Section = styled(Box)`
  margin-bottom: 24px;
`;

const ResourcesList = styled.ul`
  padding-left: 20px;
  margin: 0;
  line-height: 1.8;
`;

const ReviewsList = styled.ul`
  padding-left: 0;
  list-style: none;

  li {
    padding: 12px 0;
    border-bottom: 1px solid ${p => p.theme.colors.spotBackground[1]};

    &:last-of-type {
      border-bottom: none;
    }
  }
`;

const ReviewerPill = styled.div`
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  background: ${p => p.theme.colors.spotBackground[0]};
  color: ${p => p.theme.colors.text.main};
  font-size: 13px;

  &[data-state='APPROVED'] {
    background: ${p => p.theme.colors.success.main};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='DENIED'] {
    background: ${p => p.theme.colors.error.main};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='PROMOTED'] {
    background: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
`;

const ReviewerState = styled.span`
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  opacity: 0.75;
`;

const RoleChip = styled.button`
  background: ${p => p.theme.colors.levels.surface};
  border: 1px solid ${p => p.theme.colors.spotBackground[2]};
  color: ${p => p.theme.colors.text.main};
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;

  &[data-selected='true'] {
    background: ${p => p.theme.colors.brand};
    border-color: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
`;

const Badge = styled.span`
  display: inline-block;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;

  &[data-state='PENDING'] {
    background: ${p => p.theme.colors.warning.main};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='APPROVED'] {
    background: ${p => p.theme.colors.success.main};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='DENIED'] {
    background: ${p => p.theme.colors.error.main};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='PROMOTED'] {
    background: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
  &[data-state='NONE'] {
    background: ${p => p.theme.colors.spotBackground[1]};
    color: ${p => p.theme.colors.text.main};
  }
`;
