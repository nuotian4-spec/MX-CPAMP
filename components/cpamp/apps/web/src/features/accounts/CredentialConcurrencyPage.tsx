import { useMemo } from 'react';
import { useAuthStore } from '@/stores';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import { CredentialConcurrencyEditor } from './components/CredentialConcurrencyEditor';
import { useCredentialConcurrencySnapshot } from './hooks/useCredentialConcurrencySnapshot';

export function CredentialConcurrencyPanel({ scope }: { scope: ApiClientRequestScope }) {
  const { snapshot, error, busy, load, refresh } = useCredentialConcurrencySnapshot(scope);
  return (
    <div>
      <h1>凭证并发</h1>
      <p>分别限制每一份上游凭证。Codex与BPS独立计数；客户端访问本站的API Key规则保持原设置。</p>
      <Button onClick={refresh} disabled={busy}>
        {busy ? '正在刷新' : '刷新状态'}
      </Button>
      {error && <p role="alert">{error}</p>}
      {snapshot?.mode === 'home' && (
        <Card title="Home模式">
          <p>并发由Home管理，本页不提供本地计数和修改。</p>
        </Card>
      )}
      {snapshot?.mode === 'local' && (
        <>
          <p role="status">
            统一默认 {snapshot.default_limit} · 公共池等待 {snapshot.pool_waiting} ·
            满额最多等待30秒，超时返回429
          </p>
          {snapshot.credentials.length === 0 && <p>尚无上游凭证</p>}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit,minmax(min(100%,320px),1fr))',
              gap: 16,
              marginTop: 16,
            }}
          >
            {snapshot.credentials.map((credential) => (
              <CredentialConcurrencyEditor
                key={JSON.stringify([
                  snapshot.connection_ref,
                  credential.auth_id,
                  credential.auth_index,
                  credential.provider,
                ])}
                credential={credential}
                defaultLimit={snapshot.default_limit}
                connectionRef={snapshot.connection_ref}
                scope={scope}
                onSaved={load}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
export function CredentialConcurrencyPage() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  return <CredentialConcurrencyPanel key={`${apiBase}:${managementKey}`} scope={scope} />;
}
