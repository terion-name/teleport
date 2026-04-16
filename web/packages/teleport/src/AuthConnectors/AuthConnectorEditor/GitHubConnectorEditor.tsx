/**
 * Teleport
 * Copyright (C) 2025  Gravitational, Inc.
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

import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router';

import { useAsync } from 'shared/hooks/useAsync';

import cfg from 'teleport/config';
import { KindAuthConnectors } from 'teleport/services/resources';
import useTeleport from 'teleport/useTeleport';

import templates from '../templates';
import { AuthConnectorEditorContent } from './AuthConnectorEditorContent';

function isManagedAuthConnector(
  kind: KindAuthConnectors | undefined
): kind is 'github' | 'oidc' {
  return kind === 'github' || kind === 'oidc';
}

/**
 * AuthConnectorEditor is the edit/create page for a YAML-based auth connector.
 */
export function AuthConnectorEditor({ isNew = false }) {
  const { connectorName, connectorType } = useParams<{
    connectorName: string;
    connectorType: KindAuthConnectors;
  }>();
  const ctx = useTeleport();
  const navigate = useNavigate();
  const managedKind = isManagedAuthConnector(connectorType)
    ? connectorType
    : 'oidc';
  const initialTemplate = templates[managedKind];

  const [content, setContent] = useState(initialTemplate);
  const [initialContent, setInitialContent] = useState(initialTemplate);

  const [fetchAttempt, fetchConnector] = useAsync(async () => {
    if (!isNew) {
      const res =
        managedKind === 'oidc'
          ? await ctx.resourceService.fetchOIDCConnector(connectorName)
          : await ctx.resourceService.fetchGithubConnector(connectorName);
      setContent(res.content);
      setInitialContent(res.content);
    }
    return;
  });

  const [saveAttempt, saveConnector] = useAsync(
    useCallback(async () => {
      if (isNew) {
        await (managedKind === 'oidc'
          ? ctx.resourceService.createOIDCConnector(content)
          : ctx.resourceService.createGithubConnector(content)
        ).then(() => navigate(cfg.routes.sso));
      } else {
        await (managedKind === 'oidc'
          ? ctx.resourceService.updateOIDCConnector(connectorName, content)
          : ctx.resourceService.updateGithubConnector(connectorName, content)
        ).then(() => navigate(cfg.routes.sso));
      }
    }, [
      connectorName,
      content,
      isNew,
      managedKind,
      navigate,
      ctx.resourceService,
    ])
  );

  const isSaveDisabled =
    saveAttempt.status === 'processing' || content === initialContent;

  useEffect(() => {
    if (!isNew && !isManagedAuthConnector(connectorType)) {
      navigate(cfg.routes.sso, { replace: true });
      return;
    }

    if (fetchAttempt.status !== 'success') {
      fetchConnector();
    }
  }, [connectorType, fetchAttempt.status, fetchConnector, isNew, navigate]);

  const title = isNew
    ? `Creating new ${managedKind.toUpperCase()} Auth Connector`
    : `Editing Auth Connector: ${connectorName}`;

  return (
    <AuthConnectorEditorContent
      title={title}
      content={content}
      backButtonRoute={cfg.routes.sso}
      isSaveDisabled={isSaveDisabled}
      saveAttempt={saveAttempt}
      fetchAttempt={fetchAttempt}
      onSave={saveConnector}
      onCancel={() => navigate(cfg.routes.sso)}
      setContent={setContent}
      connectorType={managedKind}
    />
  );
}

export function GitHubConnectorEditor({ isNew = false }) {
  return <AuthConnectorEditor isNew={isNew} />;
}
