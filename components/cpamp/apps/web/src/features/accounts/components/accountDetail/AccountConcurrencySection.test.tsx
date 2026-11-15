import { useCredentialConcurrencySnapshot } from '../../hooks/useCredentialConcurrencySnapshot';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AccountRow } from '@/features/accounts/model/accountRows';
import type { CredentialConcurrency } from '@/services/api/credentialConcurrency';
import { AccountConcurrencySection } from './AccountConcurrencySection';
import { matchAccountConcurrency } from '@/features/accounts/model/credentialConcurrencyIdentity';
const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn() }));
vi.mock('@/services/api/credentialConcurrency', () => ({ credentialConcurrencyApi: api }));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'test' };
const credential: CredentialConcurrency = {
  auth_id: 'real-id',
  auth_index: 'idx',
  name: 'same@example.com',
  provider: 'codex',
  source: 'file',
  editable: true,
  read_only_reason: '',
  concurrency_limit: null,
  effective_limit: 20,
  limit_source: 'default',
  active: 3,
  pinned_waiting: 2,
};
const snapshot = {
  mode: 'local',
  connection_ref: 'ref-a',
  default_limit: 20,
  pool_waiting: 9,
  credentials: [credential],
};
const row = {
  raw: { id: 'real-id', name: 'not-the-id.json' },
  fileName: 'not-the-id.json',
  provider: 'codex',
  authIndex: 'idx',
} as AccountRow;

function PageSection({ scope: currentScope }: { scope: typeof scope }) {
  const concurrency = useCredentialConcurrencySnapshot(currentScope);
  return (
    <AccountConcurrencySection
      row={row}
      scope={currentScope}
      disabled={false}
      concurrency={concurrency}
    />
  );
}

describe('详情凭证并发', () => {
  let renderer: ReactTestRenderer | undefined;
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    api.list.mockResolvedValue(snapshot);
    api.update.mockResolvedValue({ status: 'saved' });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer?.unmount());
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  it('同邮箱不同通道仅按真实身份匹配，缺ID不按文件名猜选', () => {
    const other = { ...credential, auth_id: 'plugin-id', provider: 'cpa-bps' };
    expect(matchAccountConcurrency(row, [other, credential])).toEqual({
      status: 'matched',
      credential,
    });
    expect(matchAccountConcurrency({ ...row, raw: { name: 'real-id' } }, [credential])).toEqual({
      status: 'identity-missing',
    });
    expect(matchAccountConcurrency(row, [other])).toEqual({ status: 'missing' });
    expect(matchAccountConcurrency(row, [credential, { ...credential }])).toEqual({
      status: 'ambiguous',
    });
    expect(matchAccountConcurrency({ ...row, authIndex: 'wrong' }, [credential])).toEqual({
      status: 'missing',
    });
    expect(
      matchAccountConcurrency({ ...row, provider: 'xai' }, [{ ...credential, provider: 'grok' }])
        .status
    ).toBe('matched');
  });
  it('只展示单凭证定向等待，保存使用GET引用和当前auth_id', async () => {
    await act(async () => {
      renderer = create(<PageSection scope={scope} />);
    });
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('公共池等待');
    await act(async () => {
      renderer!.root.findByType('input').props.onChange({ target: { value: '8' } });
    });
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(api.update).toHaveBeenCalledWith('real-id', 8, 'ref-a', scope);
  });
  it('旧连接迟到读取不回填新连接', async () => {
    let resolveOld!: (value: typeof snapshot) => void;
    api.list.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        })
    );
    await act(async () => {
      renderer = create(<PageSection scope={scope} />);
    });
    const nextScope = { ...scope, apiBase: 'https://next.invalid' };
    api.list.mockResolvedValue({
      ...snapshot,
      connection_ref: 'ref-b',
      credentials: [{ ...credential, active: 6 }],
    });
    await act(async () => {
      renderer!.update(<PageSection scope={nextScope} />);
    });
    await act(async () => {
      resolveOld(snapshot);
    });
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(api.update).toHaveBeenCalledWith('real-id', null, 'ref-b', nextScope);
  });
});
