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

import { useEffect, useState } from 'react';
import styled from 'styled-components';

import {
  Alert,
  Box,
  ButtonPrimary,
  ButtonSecondary,
  Flex,
  Indicator,
  Label as Pill,
  Text,
} from 'design';
import FieldInput from 'shared/components/FieldInput';
import Validation from 'shared/components/Validation';

import type { AccessRequestResourceIdPayload } from 'teleport/services/accessRequests';
import useTeleport from 'teleport/useTeleport';

import { ResourcePicker } from './ResourcePicker';

type Props = {
  clusterId: string;
  onCreated: () => void;
  onCancel: () => void;
};

/**
 * CreateAccessRequest is a minimal form for creating a new request.
 * Supports role-based requests and resource-based requests (via resource IDs).
 */
export function CreateAccessRequest({ clusterId, onCancel, onCreated }: Props) {
  const ctx = useTeleport();

  const [availableRoles, setAvailableRoles] = useState<
    Array<{ name: string; description: string }>
  >([]);
  const [loadingRoles, setLoadingRoles] = useState(true);
  const [rolesError, setRolesError] = useState<string | null>(null);

  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [reason, setReason] = useState('');
  const [resourceIds, setResourceIds] = useState('');
  const [pickedResources, setPickedResources] = useState<
    AccessRequestResourceIdPayload[]
  >([]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [durationHours, setDurationHours] = useState('');
  const [startLocal, setStartLocal] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setLoadingRoles(true);
      setRolesError(null);
      try {
        const resp =
          await ctx.accessRequestService.fetchRequestableRoles(clusterId);
        if (!cancelled) setAvailableRoles(resp.roles);
      } catch (e) {
        if (!cancelled) setRolesError((e as Error).message);
      } finally {
        if (!cancelled) setLoadingRoles(false);
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [clusterId, ctx.accessRequestService]);

  function toggleRole(name: string) {
    setSelectedRoles(roles =>
      roles.includes(name) ? roles.filter(r => r !== name) : [...roles, name]
    );
  }

  function parseResources() {
    // Accept comma or newline separated "kind/name[@cluster]" or JSON array.
    const trimmed = resourceIds.trim();
    if (!trimmed) return [];
    if (trimmed.startsWith('[')) {
      try {
        return JSON.parse(trimmed);
      } catch (e) {
        throw new Error(`Invalid resources JSON: ${(e as Error).message}`);
      }
    }
    return trimmed
      .split(/[\n,]/)
      .map(s => s.trim())
      .filter(Boolean)
      .map(line => {
        const [kindName, cluster] = line.split('@');
        const parts = kindName.split('/');
        if (parts.length < 2) {
          throw new Error(
            `Invalid resource line "${line}". Expected "kind/name[@clusterName]".`
          );
        }
        return {
          kind: parts[0],
          name: parts.slice(1).join('/'),
          clusterName: cluster || clusterId,
        };
      });
  }

  async function submit() {
    setSubmitting(true);
    setSubmitError(null);
    try {
      const textResources = parseResources();
      // Union picked + typed, dedup on (kind, name, clusterName).
      const all = [...pickedResources, ...textResources];
      const seen = new Set<string>();
      const resources = all.filter(r => {
        const k = `${r.kind}|${r.name}|${r.clusterName}`;
        if (seen.has(k)) return false;
        seen.add(k);
        return true;
      });
      if (selectedRoles.length === 0 && resources.length === 0) {
        throw new Error(
          'Request must include at least one role or one resource.'
        );
      }

      let maxDuration: string | undefined;
      if (durationHours.trim()) {
        const hours = Number(durationHours);
        if (!Number.isFinite(hours) || hours <= 0) {
          throw new Error(
            `Invalid duration "${durationHours}". Enter a positive number of hours.`
          );
        }
        const cap = new Date(Date.now() + hours * 60 * 60 * 1000);
        maxDuration = cap.toISOString();
      }

      let assumeStartTime: string | undefined;
      if (startLocal.trim()) {
        const start = new Date(startLocal);
        if (!Number.isFinite(start.getTime())) {
          throw new Error(
            `Invalid start time "${startLocal}". Use the date/time picker.`
          );
        }
        if (start.getTime() <= Date.now()) {
          throw new Error('Start time must be in the future.');
        }
        assumeStartTime = start.toISOString();
      }

      await ctx.accessRequestService.createAccessRequest(clusterId, {
        roles: selectedRoles,
        resources,
        requestReason: reason,
        maxDuration,
        assumeStartTime,
      });
      onCreated();
    } catch (e) {
      setSubmitError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Validation>
      <Box>
        {submitError && <Alert kind="danger">{submitError}</Alert>}
        <Section>
        <Text typography="h4" mb={2}>
          Requested roles
        </Text>
        {rolesError && <Alert kind="danger">{rolesError}</Alert>}
        {loadingRoles ? (
          <Indicator />
        ) : availableRoles.length === 0 ? (
          <Text color="text.slightlyMuted">
            No roles are available to request. Roles you already hold are
            hidden. Ask a cluster admin to add{' '}
            <code>allow.request.roles</code> to one of your roles (and make
            sure the target role is not already assigned to you).
          </Text>
        ) : (
          <RoleChips>
            {availableRoles.map(r => (
              <RoleChip
                key={r.name}
                onClick={() => toggleRole(r.name)}
                data-selected={selectedRoles.includes(r.name)}
                title={r.description}
              >
                {r.name}
              </RoleChip>
            ))}
          </RoleChips>
        )}
        {selectedRoles.length > 0 && (
          <Flex mt={3} gap={1} flexWrap="wrap">
            <Text typography="body2" color="text.slightlyMuted">
              Selected:
            </Text>
            {selectedRoles.map(r => (
              <Pill key={r} kind="primary">
                {r}
              </Pill>
            ))}
          </Flex>
        )}
      </Section>

      <Section>
        <Text typography="h4" mb={2}>
          Resources (optional)
        </Text>
        <Flex gap={2} mb={2}>
          <ButtonSecondary onClick={() => setPickerOpen(true)}>
            Browse resources
          </ButtonSecondary>
        </Flex>

        {pickedResources.length > 0 && (
          <Box mb={3}>
            <Text typography="body2" color="text.slightlyMuted" mb={1}>
              Selected:
            </Text>
            <Flex gap={1} flexWrap="wrap">
              {pickedResources.map((r, i) => (
                <Pill key={i} kind="primary">
                  {r.kind}/{r.name}
                  <span
                    role="button"
                    tabIndex={0}
                    onClick={e => {
                      e.stopPropagation();
                      setPickedResources(prev =>
                        prev.filter((_, j) => j !== i)
                      );
                    }}
                    style={{
                      marginLeft: 6,
                      cursor: 'pointer',
                      fontWeight: 'bold',
                    }}
                  >
                    ×
                  </span>
                </Pill>
              ))}
            </Flex>
          </Box>
        )}

        <Text typography="body2" color="text.slightlyMuted" mb={2}>
          Or type resource IDs manually, one per line, format:{' '}
          <code>kind/name[@clusterName]</code>.
        </Text>
        <FieldInput
          placeholder={`node/db-01\napp/grafana`}
          value={resourceIds}
          onChange={e => setResourceIds(e.target.value)}
        />
      </Section>

      {pickerOpen && (
        <ResourcePicker
          clusterId={clusterId}
          onAdd={added => {
            setPickedResources(prev => {
              const keys = new Set(
                prev.map(p => `${p.kind}|${p.name}|${p.clusterName}`)
              );
              const merged = [...prev];
              for (const r of added) {
                const k = `${r.kind}|${r.name}|${r.clusterName}`;
                if (!keys.has(k)) {
                  merged.push(r);
                  keys.add(k);
                }
              }
              return merged;
            });
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}

      <Section>
        <Text typography="h4" mb={2}>
          Duration (optional)
        </Text>
        <Text typography="body2" color="text.slightlyMuted" mb={2}>
          How long the approved access should last, in hours. Fractional
          values are allowed (e.g. <code>0.5</code> for 30 minutes). Leave
          empty to use the cluster default. The effective duration is also
          capped by <code>max_duration</code> on your role.
        </Text>
        <DurationRow>
          <FieldInput
            type="text"
            inputMode="decimal"
            placeholder="8"
            value={durationHours}
            onChange={e => setDurationHours(e.target.value)}
          />
          <Text typography="body2" color="text.slightlyMuted">
            hours
          </Text>
        </DurationRow>
      </Section>

      <Section>
        <Text typography="h4" mb={2}>
          Start time (optional)
        </Text>
        <Text typography="body2" color="text.slightlyMuted" mb={2}>
          Schedule the access to begin in the future. Leave empty to make
          the roles available as soon as the request is approved.
        </Text>
        <DateTimeInput
          type="datetime-local"
          value={startLocal}
          onChange={e => setStartLocal(e.target.value)}
        />
      </Section>

      <Section>
        <Text typography="h4" mb={2}>
          Reason
        </Text>
        <FieldInput
          placeholder="Why do you need this access?"
          value={reason}
          onChange={e => setReason(e.target.value)}
        />
      </Section>

      <Flex gap={2} mt={4}>
        <ButtonPrimary disabled={submitting} onClick={submit}>
          {submitting ? 'Creating…' : 'Create request'}
        </ButtonPrimary>
        <ButtonSecondary onClick={onCancel}>Cancel</ButtonSecondary>
        </Flex>
      </Box>
    </Validation>
  );
}

const Section = styled(Box)`
  margin-bottom: 24px;
`;

const RoleChips = styled(Flex)`
  flex-wrap: wrap;
  gap: 8px;
`;

const DurationRow = styled(Flex)`
  align-items: center;
  gap: 8px;
  max-width: 240px;
`;

const DateTimeInput = styled.input`
  background: ${p => p.theme.colors.levels.surface};
  color: ${p => p.theme.colors.text.main};
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
  padding: 8px 10px;
  font-size: 13px;
  font-family: inherit;
`;

const RoleChip = styled.button`
  border: 1px solid ${p => p.theme.colors.spotBackground[2]};
  background: ${p => p.theme.colors.levels.surface};
  color: ${p => p.theme.colors.text.main};
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  transition:
    background 0.15s,
    border-color 0.15s;

  &:hover {
    background: ${p => p.theme.colors.spotBackground[0]};
  }

  &[data-selected='true'] {
    background: ${p => p.theme.colors.brand};
    border-color: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
`;
