import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloEgressFields } from './KiloEgressFields';
import { KiloEgressSummary } from './KiloEgressSummary';
import { KiloQuotaSummary } from './KiloQuotaSummary';
import { useCredentialConcurrencySnapshot } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import { CredentialConcurrencyBadge } from '@/features/accounts/components/CredentialConcurrencyBadge';
import { AccountConcurrencySection } from '@/features/accounts/components/accountDetail/AccountConcurrencySection';
import type { AccountRow } from '@/features/accounts/model/accountRows';
import type { KiloAccount } from '@/services/api/kilo';
import type { KiloEgressInput, KiloProxyState } from '@/services/api/kiloProxies';
import type { ConcurrencySnapshot } from '@/services/api/credentialConcurrency';

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn() }));
vi.mock('@/services/api/credentialConcurrency', () => ({ credentialConcurrencyApi: api }));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'http://fixture.invalid', managementKey: 'SYNTHETIC' };
const row = (provider: string) =>
  ({ raw: { id: provider }, provider, authIndex: provider, selectionKey: provider }) as AccountRow;
const snapshot: ConcurrencySnapshot = {
  mode: 'local',
  connection_ref: 'ref-a',
  default_limit: 20,
  pool_waiting: 9,
  credentials: ['codex', 'kilo', 'cpa-bps'].map((provider) => ({
    auth_id: provider,
    auth_index: provider,
    name: '同名账号',
    provider,
    source: 'file',
    editable: true,
    read_only_reason: '',
    concurrency_limit: null,
    effective_limit: 20,
    limit_source: 'default',
    active: 3,
    pinned_waiting: 2,
  })),
};
const proxies = {
  revision: 7,
  connection_ref: 'ref-a',
  accounts: [
    { account_id: 'kilo', mode: 'pool', primary_proxy_id: 'p1', candidate_proxy_ids: ['p1', 'p2'] },
  ],
  proxies: [
    {
      id: 'p1',
      name: '共享一',
      account_id: '',
      host: '127.0.0.1',
      port: '8080',
      active: 0,
      owner: '',
      observed_exit_ip: null,
    },
    {
      id: 'p2',
      name: '专属二',
      account_id: 'kilo',
      host: '127.0.0.2',
      port: '8080',
      active: 0,
      owner: 'kilo',
      owner_kind: 'account',
    },
  ],
} as KiloProxyState;
let renderer: ReactTestRenderer;
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  api.list.mockReset().mockResolvedValue(snapshot);
  api.update.mockReset().mockResolvedValue({ status: 'saved' });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('UX1共享运行快照', () => {
  function Page({ currentScope = scope }: { currentScope?: typeof scope }) {
    const state = useCredentialConcurrencySnapshot(currentScope);
    return (
      <>
        {['codex', 'kilo', 'cpa-bps'].map((provider) => (
          <CredentialConcurrencyBadge key={provider} row={row(provider)} state={state} />
        ))}
        <AccountConcurrencySection
          row={row('codex')}
          scope={currentScope}
          disabled={false}
          concurrency={state}
        />
      </>
    );
  }
  it('三provider卡片和详情共用一个5秒订阅，隐藏暂停；失败保留陈旧值，Home未知', async () => {
    const events = new EventTarget();
    const document = {
      visibilityState: 'visible',
      addEventListener: events.addEventListener.bind(events),
      removeEventListener: events.removeEventListener.bind(events),
    };
    vi.stubGlobal('document', document);
    await act(async () => {
      renderer = create(<Page />);
    });
    expect(api.list).toHaveBeenCalledTimes(1);
    expect(
      renderer.root
        .findAllByProps({ role: 'status' })
        .filter((node) => JSON.stringify(node.children).includes('当前并发'))
    ).toHaveLength(3);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(api.list).toHaveBeenCalledTimes(2);
    document.visibilityState = 'hidden';
    events.dispatchEvent(new Event('visibilitychange'));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(api.list).toHaveBeenCalledTimes(2);
    api.list.mockRejectedValueOnce(new Error('offline'));
    await act(async () => {
      document.visibilityState = 'visible';
      events.dispatchEvent(new Event('visibilitychange'));
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('状态陈旧');
    api.list.mockResolvedValue({ mode: 'home', credentials: [] });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('Home管理');
    expect(JSON.stringify(renderer.toJSON())).not.toContain('共享池等待');
  });
  it('切换连接取消旧请求，迟到响应不覆盖新连接', async () => {
    let resolve!: (value: ConcurrencySnapshot) => void;
    api.list.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        })
    );
    await act(async () => {
      renderer = create(<Page />);
    });
    const signal = api.list.mock.calls[0][1] as AbortSignal;
    api.list.mockResolvedValue({
      ...snapshot,
      connection_ref: 'ref-b',
      credentials: snapshot.credentials.map((item) => ({ ...item, active: 8 })),
    });
    await act(async () => {
      renderer.update(<Page currentScope={{ ...scope, apiBase: 'http://next.invalid' }} />);
    });
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolve(snapshot);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('8');
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(api.update.mock.calls[0][2]).toBe('ref-b');
  });
});

describe('UX1来源和出口', () => {
  const account = {
    id: 'kilo',
    quota_source_summary: {
      kind: 'cpa_codex',
      credential_id: 'codex-real',
      display_label: '同名Codex',
      status: 'ready',
      binding_revision: 2,
    },
    quota_snapshot: {
      account_id: 'kilo',
      binding_revision: 2,
      connection_ref: 'ref-a',
      source: { kind: 'cpa_codex', credential_id: 'codex-real', status: 'ready' },
      query: {
        status: 'success',
        attempted_at: null,
        observed_at: '2026-11-13T01:02:00Z',
        error_code: null,
      },
      plan: null,
      windows: [
        {
          group: 'rate_limit',
          slot: 'primary_window',
          duration_seconds: 18000,
          remaining_percent: 42,
          reset_at: null,
          source: 'api_query',
        },
        {
          group: 'additional_rate_limits:0',
          slot: 'primary_window',
          duration_seconds: 604800,
          remaining_percent: 99,
          reset_at: null,
          source: 'api_query',
        },
      ],
      credits: { remaining: null, source_field: null, unit: null },
      stale: false,
    },
  } as KiloAccount;
  it('仅主窗口摘要；绑定变更隐藏旧窗口，缺省来源保持未知', async () => {
    await act(async () => {
      renderer = create(<KiloQuotaSummary account={account} />);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('42%');
    expect(JSON.stringify(renderer.toJSON())).not.toContain('99%');
    await act(async () => {
      renderer.update(
        <KiloQuotaSummary
          account={{
            ...account,
            quota_source_summary: { ...account.quota_source_summary!, binding_revision: 3 },
          }}
        />
      );
    });
    expect(JSON.stringify(renderer.toJSON())).not.toContain('42%');
    await act(async () => {
      renderer.update(
        <KiloQuotaSummary account={{ ...account, quota_source_summary: undefined }} />
      );
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('额度来源未读取');
    expect(JSON.stringify(renderer.toJSON())).not.toContain('未关联Codex');
  });
  it('同绑定查询后来源需登录时采用新状态并标历史观测', async () => {
    const updated = {
      ...account,
      quota_snapshot: {
        ...account.quota_snapshot!,
        source: { ...account.quota_snapshot!.source, status: 'needs_sign_in' as const },
      },
    };
    await act(async () => {
      renderer = create(<KiloQuotaSummary account={updated} />);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('需重新登录');
    expect(JSON.stringify(renderer.toJSON())).toContain('历史观测');
    expect(JSON.stringify(renderer.toJSON())).not.toContain('可查询');
  });
  it('专属owner保留但active为0仍显示空闲', async () => {
    await act(async () => {
      renderer = create(<KiloEgressSummary accountID="kilo" snapshot={proxies} />);
    });
    expect(renderer.root.findByType('details').props.open).toBeUndefined();
    expect(JSON.stringify(renderer.toJSON())).toContain('当前空闲');
  });
  it('候选多选不自动独占，去掉主代理同步清主偏好和独占子集', async () => {
    const result = (): KiloEgressInput => renderer.root.findByType(KiloEgressFields).props.value;
    function Form() {
      const [value, setValue] = useState<KiloEgressInput>({
        mode: 'pool',
        candidate_proxy_ids: ['p1', 'p2'],
        exclusive_proxy_ids: [],
        primary_proxy_id: 'p1',
      });
      return (
        <KiloEgressFields
          snapshot={proxies}
          accountID="kilo"
          value={value}
          onChange={setValue}
          disabled={false}
        />
      );
    }
    await act(async () => {
      renderer = create(<Form />);
    });
    expect(result().exclusive_proxy_ids).toEqual([]);
    await act(async () => {
      renderer.root.findAllByType(ToggleSwitch)[0].props.onChange(true);
    });
    expect(result().exclusive_proxy_ids).toEqual(['p1']);
    await act(async () => {
      renderer.root.findAllByType('input')[0].props.onChange({ target: { checked: false } });
    });
    expect(result()).toEqual({
      mode: 'pool',
      candidate_proxy_ids: ['p2'],
      exclusive_proxy_ids: [],
      primary_proxy_id: '',
    });
    await act(async () => {
      renderer.root
        .findAllByType(Select)
        .find((item) => item.props.ariaLabel === '出口策略')!
        .props.onChange('direct');
    });
    expect(result()).toEqual({
      capacity_mode: 'fixed',
      allocation_mode: 'auto',
      max_ips: 1,
      specified_ips: [],
      mode: 'direct',
      candidate_proxy_ids: null,
      exclusive_proxy_ids: [],
      primary_proxy_id: '',
    });
  });
});
