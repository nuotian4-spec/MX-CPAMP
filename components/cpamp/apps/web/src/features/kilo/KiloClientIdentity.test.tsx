import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KiloClientIdentity } from './KiloClientIdentity';
import { KiloAccountsPanel } from './KiloAccountsPage';
import { KiloEgressSummary } from './KiloEgressSummary';
import { Button } from '@/components/ui/Button';
import type {
  KiloAccount,
  KiloAccountsSnapshot,
  KiloClientIdentity as Identity,
} from '@/services/api/kilo';
import type { PropsWithChildren } from 'react';

const mocks = vi.hoisted(() => ({ list: vi.fn(), proxies: vi.fn() }));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: { list: mocks.list },
}));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: { list: mocks.proxies },
}));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('@/features/accounts/hooks/useCredentialConcurrencySnapshot', () => ({
  useCredentialConcurrencySnapshot: () => ({}),
}));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: PropsWithChildren) => <div role="dialog">{children}</div>,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));

const identity: Identity = {
  state: 'persisted',
  profiles: [
    { ref: 'profile-1', exits: [{ kind: 'ip', ip: '8.8.8.8' }, { kind: 'direct' }] },
    { ref: 'profile-2', exits: [] },
    { ref: 'profile-3' },
    { ref: 'profile-4', exits: [{ kind: 'legacy' }] },
  ],
};
const scope = { apiBase: 'http://fixture.invalid', managementKey: 'I3_SYNTHETIC' };
const account: KiloAccount = {
  id: 'i3-account.json',
  name: '合成身份账号',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
  client_identity: identity,
};
let renderer: ReactTestRenderer | undefined;
const text = () => JSON.stringify(renderer?.toJSON());
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  mocks.list.mockReset().mockResolvedValue({ connection_ref: 'ref-a', accounts: [account] });
  mocks.proxies.mockReset().mockResolvedValue({
    connection_ref: 'ref-a',
    revision: 1,
    runtime_revision: 1,
    managed: true,
    proxy_limit: 10,
    accounts: [],
    proxies: [],
    recycle: [],
  });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});

describe('I3 客户端身份只读片段', () => {
  it.each([undefined, null])('旧字段缺失 %s 不合成待初始化', async (value) => {
    await act(async () => {
      renderer = create(<KiloClientIdentity identity={value} />);
    });
    expect(text()).toContain('身份信息未返回');
    expect(text()).not.toContain('待初始化');
  });
  it.each([
    ['pending', '待初始化'],
    ['invalid', '异常'],
  ] as const)('显示 %s 且不创建操作入口', async (state, label) => {
    await act(async () => {
      renderer = create(<KiloClientIdentity identity={{ state, profiles: [] }} />);
    });
    expect(text()).toContain(label);
    expect(renderer!.root.findAllByType('button')).toHaveLength(0);
    expect(renderer!.root.findAllByType('li')).toHaveLength(0);
  });
  it('区别未返回关联、无关联、直连与旧出口，保留闲置档案', async () => {
    await act(async () => {
      renderer = create(<KiloClientIdentity identity={identity} />);
    });
    expect(text()).toContain('已持久化');
    expect(text()).toContain('4 个档案');
    expect(text()).toContain('出口关联未返回');
    expect(text()).toContain('无出口关联');
    expect(text()).toContain('旧出口关联，公网 IP 待核');
    expect(text()).toContain('8.8.8.8、直连');
    expect(renderer!.root.findAllByType('li')).toHaveLength(4);
    expect(text()).not.toContain('档案并发');
  });
  it('列表首屏不新增身份面板，原详情与内嵌账号接入同一片段', async () => {
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} />);
    });
    expect(renderer!.root.findAllByType(KiloClientIdentity)).toHaveLength(0);
    expect(renderer!.root.findByType(KiloEgressSummary).props.identity).toEqual(identity);
    const detail = renderer!.root
      .findAllByType(Button)
      .find((button) => button.props.children === '查看详情')!;
    await act(async () => {
      detail.props.onClick();
    });
    expect(renderer!.root.findByType(KiloClientIdentity).props.identity).toEqual(identity);
    await act(async () => {
      renderer!.unmount();
      renderer = create(<KiloAccountsPanel scope={scope} accountID={account.id} />);
    });
    expect(renderer!.root.findByType(KiloClientIdentity).props.identity).toEqual(identity);
  });
  it('换连接清空旧身份并丢弃迟到列表，不跨连接拼接出口', async () => {
    let resolveA!: (value: KiloAccountsSnapshot) => void;
    const pending = new Promise<KiloAccountsSnapshot>((resolve) => {
      resolveA = resolve;
    });
    mocks.list.mockReturnValueOnce(pending).mockResolvedValueOnce({
      connection_ref: 'ref-b',
      accounts: [{ ...account, client_identity: { state: 'pending', profiles: [] } }],
    });
    const nextScope = { ...scope, managementKey: 'I3_OTHER' };
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} accountID={account.id} />);
    });
    await act(async () => {
      renderer!.update(<KiloAccountsPanel scope={nextScope} accountID={account.id} />);
    });
    await act(async () => {
      resolveA({ connection_ref: 'ref-a', accounts: [account] });
    });
    expect(text()).toContain('待初始化');
    expect(text()).not.toContain('档案 1');
    expect(renderer!.root.findByType(KiloEgressSummary).props.snapshot).toBeUndefined();
  });
});
