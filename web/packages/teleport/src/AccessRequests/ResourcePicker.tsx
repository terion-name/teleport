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

import api from 'teleport/services/api';

import type { AccessRequestResourceIdPayload } from 'teleport/services/accessRequests';

type Props = {
  clusterId: string;
  onAdd: (resources: AccessRequestResourceIdPayload[]) => void;
  onClose: () => void;
};

type UnifiedItem = {
  kind: string;
  // Servers (nodes) use "id" + "hostname".
  id?: string;
  hostname?: string;
  // Apps / DBs / Kubes use "name".
  name?: string;
  // Labels shape: [{ name, value }]
  labels?: Array<{ name: string; value: string }>;
  // Desktops use "name".
};

/**
 * ResourcePicker is a lightweight modal for adding requestable resources to
 * an access request. It fetches one page of unified resources with
 * `searchAsRoles=yes` so requestable-but-not-currently-accessible resources
 * are included, and lets the user multi-select.
 *
 * This is intentionally minimal compared to the full UnifiedResources view:
 * no pagination UI, no pinning, no preferences. The text fallback still
 * works in CreateAccessRequest if the user prefers typing IDs directly.
 */
export function ResourcePicker({ clusterId, onAdd, onClose }: Props) {
  const [items, setItems] = useState<UnifiedItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<AccessRequestResourceIdPayload[]>(
    []
  );

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setLoading(true);
      setError(null);
      try {
        const params = new URLSearchParams();
        params.set('limit', '200');
        params.set('searchAsRoles', 'yes');
        params.set('includedResourceMode', 'requestable');
        if (search.trim()) params.set('query', search.trim());
        const json = await api.get(
          `/v1/webapi/sites/${encodeURIComponent(clusterId)}/resources?${params.toString()}`
        );
        if (!cancelled) {
          setItems((json.items || json.agents || []) as UnifiedItem[]);
        }
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [clusterId, search]);

  function toggle(item: UnifiedItem) {
    const rid = toResourceId(item, clusterId);
    if (!rid) return;
    setSelected(prev => {
      const exists = prev.some(
        p =>
          p.kind === rid.kind &&
          p.name === rid.name &&
          p.clusterName === rid.clusterName
      );
      if (exists) {
        return prev.filter(
          p =>
            !(
              p.kind === rid.kind &&
              p.name === rid.name &&
              p.clusterName === rid.clusterName
            )
        );
      }
      return [...prev, rid];
    });
  }

  function isSelected(item: UnifiedItem): boolean {
    const rid = toResourceId(item, clusterId);
    if (!rid) return false;
    return selected.some(
      p =>
        p.kind === rid.kind &&
        p.name === rid.name &&
        p.clusterName === rid.clusterName
    );
  }

  return (
    <Backdrop onClick={onClose}>
      <Modal onClick={e => e.stopPropagation()}>
        <Flex justifyContent="space-between" alignItems="center" mb={3}>
          <Text typography="h3">Add resources</Text>
          <ButtonSecondary onClick={onClose}>Close</ButtonSecondary>
        </Flex>

        <SearchInput
          placeholder="Search by name or label…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />

        {error && (
          <Alert kind="danger" mt={2}>
            {error}
          </Alert>
        )}

        <ItemsBox>
          {loading ? (
            <Box textAlign="center" p={4}>
              <Indicator />
            </Box>
          ) : items.length === 0 ? (
            <Text color="text.slightlyMuted" p={4} textAlign="center">
              No resources match. Resources shown here are filtered to what
              you can request via <code>search_as_roles</code>.
            </Text>
          ) : (
            items.map((item, i) => (
              <ItemRow
                key={i}
                data-selected={isSelected(item)}
                onClick={() => toggle(item)}
              >
                <Flex alignItems="center" gap={2}>
                  <KindBadge>{item.kind}</KindBadge>
                  <Text>{displayName(item)}</Text>
                </Flex>
                <Flex gap={1} flexWrap="wrap">
                  {(item.labels || []).slice(0, 5).map((l, j) => (
                    <Pill key={j} kind="secondary">
                      {l.name}={l.value}
                    </Pill>
                  ))}
                </Flex>
              </ItemRow>
            ))
          )}
        </ItemsBox>

        <Flex justifyContent="space-between" alignItems="center" mt={3}>
          <Text typography="body2" color="text.slightlyMuted">
            {selected.length} selected
          </Text>
          <Flex gap={2}>
            <ButtonSecondary onClick={onClose}>Cancel</ButtonSecondary>
            <ButtonPrimary
              disabled={selected.length === 0}
              onClick={() => {
                onAdd(selected);
                onClose();
              }}
            >
              Add {selected.length || ''} resource
              {selected.length === 1 ? '' : 's'}
            </ButtonPrimary>
          </Flex>
        </Flex>
      </Modal>
    </Backdrop>
  );
}

function displayName(item: UnifiedItem): string {
  return item.hostname || item.name || item.id || '(unknown)';
}

function toResourceId(
  item: UnifiedItem,
  clusterName: string
): AccessRequestResourceIdPayload | null {
  const name = item.id || item.name;
  if (!name) return null;
  // Map UI kind labels to access-request resource kinds. Values here mirror
  // what the auth server expects in ResourceID.Kind.
  const kind = item.kind;
  return { kind, name, clusterName };
}

const Backdrop = styled(Box)`
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
`;

const Modal = styled(Box)`
  background: ${p => p.theme.colors.levels.surface};
  border-radius: 8px;
  padding: 24px;
  width: min(720px, 100%);
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.4);
`;

const SearchInput = styled.input`
  background: ${p => p.theme.colors.levels.deep};
  color: ${p => p.theme.colors.text.main};
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
  padding: 8px 12px;
  font-size: 14px;
  width: 100%;
`;

const ItemsBox = styled(Box)`
  flex: 1;
  overflow-y: auto;
  margin-top: 12px;
  border: 1px solid ${p => p.theme.colors.spotBackground[1]};
  border-radius: 4px;
`;

const ItemRow = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  cursor: pointer;
  border-bottom: 1px solid ${p => p.theme.colors.spotBackground[1]};

  &:last-of-type {
    border-bottom: none;
  }

  &:hover {
    background: ${p => p.theme.colors.spotBackground[0]};
  }

  &[data-selected='true'] {
    background: ${p => p.theme.colors.brand};
    color: ${p => p.theme.colors.text.primaryInverse};
  }
`;

const KindBadge = styled.span`
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  background: ${p => p.theme.colors.spotBackground[2]};
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
`;
