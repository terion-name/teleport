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

import styled from 'styled-components';

import { ButtonSecondary, Flex, Label as Pill, Text } from 'design';
import Table, { Cell } from 'design/DataTable';
import { AccessRequest, RequestState } from 'shared/services/accessRequests';

type Props = {
  requests: AccessRequest[];
  currentUser: string;
  onView: (id: string) => void;
};

export function RequestsTable({ requests, currentUser, onView }: Props) {
  return (
    <Table
      data={requests}
      emptyText="No access requests"
      columns={[
        {
          key: 'user',
          headerText: 'User',
          render: (req: AccessRequest) => (
            <Cell>
              <UserCell>
                {req.user}
                {req.user === currentUser && (
                  <Pill kind="secondary" ml={2}>
                    you
                  </Pill>
                )}
              </UserCell>
            </Cell>
          ),
        },
        {
          key: 'roles',
          headerText: 'Requested',
          render: (req: AccessRequest) => (
            <Cell>
              <RolesList>
                {req.roles.length > 0 ? (
                  req.roles.map(r => (
                    <Pill key={r} kind="secondary" mr={1}>
                      {r}
                    </Pill>
                  ))
                ) : (
                  <Text
                    typography="body2"
                    color="text.slightlyMuted"
                  >{`${req.resources.length} resource(s)`}</Text>
                )}
              </RolesList>
            </Cell>
          ),
        },
        {
          key: 'state',
          headerText: 'Status',
          render: (req: AccessRequest) => (
            <Cell>
              <StateLabel state={req.state}>{req.state}</StateLabel>
            </Cell>
          ),
        },
        {
          key: 'createdDuration',
          headerText: 'Created',
          render: (req: AccessRequest) => (
            <Cell>{req.createdDuration || '—'}</Cell>
          ),
        },
        {
          altKey: 'actions',
          headerText: '',
          render: (req: AccessRequest) => (
            <Cell align="right">
              <ButtonSecondary onClick={() => onView(req.id)}>
                View
              </ButtonSecondary>
            </Cell>
          ),
        },
      ]}
    />
  );
}

function StateLabel({
  state,
  children,
}: {
  state: RequestState;
  children: React.ReactNode;
}) {
  return <StateDot data-state={state}>{children}</StateDot>;
}

const UserCell = styled(Flex)`
  align-items: center;
`;

const RolesList = styled(Flex)`
  flex-wrap: wrap;
  gap: 4px;
`;

const StateDot = styled.span`
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
