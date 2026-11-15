import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KiloEgressSummary } from './KiloEgressSummary';
import type { KiloClientIdentity } from '@/services/api/kilo';
import type { KiloProxyState } from '@/services/api/kiloProxies';

const identity: KiloClientIdentity = {
  state: 'persisted',
  profiles: [
    { ref: 'profile-1', exits: [{ kind: 'ip', ip: '8.8.8.8' }] },
    { ref: 'profile-2', exits: [{ kind: 'direct' }] },
    { ref: 'profile-3', exits: [] },
    { ref: 'profile-4', exits: [{ kind: 'legacy' }] },
  ],
};
const snapshot: KiloProxyState = {
  revision: 3,
  runtime_revision: 3,
  managed: true,
  proxy_limit: 10,
  connection_ref: 'ref-a',
  accounts: [
    {
      account_id: 'account-a',
      mode: 'auto',
      primary_proxy_id: '',
      assigned_ips: ['8.8.8.8'],
      max_ips: 2,
      capacity_mode: 'ip',
      configured_capacity: 40,
      current_capacity: 20,
    },
  ],
  proxies: ['p1', 'p2'].map((id) => ({
    id,
    name: id,
    account_id: '',
    order: 0,
    scheme: 'http',
    host: '127.0.0.1',
    port: '8080',
    has_auth: false,
    enabled: true,
    healthy: true,
    active: 0,
    owner: '',
    fixed_exit_ip: '8.8.8.8',
    observed_exit_ip: '8.8.8.8',
  })),
  recycle: [],
  ip_groups: [
    {
      ip: '8.8.8.8',
      owner_account_id: 'account-a',
      account_ids: ['account-a'],
      draining: false,
      retired: false,
      reason: '',
      unique_accounts: 1,
      max_unique_accounts: 3,
      state: 'assigned',
      proxy_ids: ['p1', 'p2'],
      inference_active: 2,
      management_active: 1,
      capacity: 20,
    },
  ],
};
let renderer: ReactTestRenderer;
const text = () => JSON.stringify(renderer.toJSON());
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  vi.unstubAllGlobals();
});

describe('I3 正式出口精确身份关联', () => {
  it('auto同IP双入口只占一行，闲置和直连档案不扩容', async () => {
    await act(async () => {
      renderer = create(
        <KiloEgressSummary
          accountID="account-a"
          identity={identity}
          snapshot={snapshot}
          stale
          readAt={1700000000000}
        />
      );
    });
    expect(renderer.root.findAllByType('li')).toHaveLength(1);
    expect(text()).toContain('档案 1');
    expect(text()).not.toContain('档案 2');
    expect(text()).not.toContain('档案 3');
    expect(text()).not.toContain('档案 4');
    expect(text()).toContain('已分配 1/2 IP');
    expect(text()).toContain('快照陈旧');
    expect(text()).toContain('出口快照');
    expect(text()).not.toContain('档案并发');
    expect(snapshot.accounts[0].current_capacity).toBe(20);
  });
  it('共享池保留两个入口且重复同一安全档案，不使用legacy猜配', async () => {
    const pool = {
      ...snapshot,
      accounts: [
        {
          account_id: 'account-a',
          mode: 'pool' as const,
          primary_proxy_id: '',
          candidate_proxy_ids: ['p1', 'p2'],
        },
      ],
    };
    await act(async () => {
      renderer = create(
        <KiloEgressSummary accountID="account-a" identity={identity} snapshot={pool} />
      );
    });
    expect(renderer.root.findAllByType('li')).toHaveLength(2);
    expect(text().match(/档案 1/g)).toHaveLength(2);
    expect(text()).not.toContain('档案 4');
    expect(text()).toContain('指定 2 个入口');
  });
  it('缺失身份与缺失关系均不回退首档案', async () => {
    await act(async () => {
      renderer = create(<KiloEgressSummary accountID="account-a" snapshot={snapshot} />);
    });
    expect(text()).toContain('身份信息未返回');
    expect(text()).toContain('档案关联待核');
    await act(async () => {
      renderer.update(
        <KiloEgressSummary
          accountID="account-a"
          snapshot={snapshot}
          identity={{
            state: 'persisted',
            profiles: [{ ref: 'profile-1' }, { ref: 'profile-2', exits: [{ kind: 'legacy' }] }],
          }}
        />
      );
    });
    expect(text()).toContain('档案关联待核');
    expect(text()).not.toContain('档案 1');
    expect(text()).not.toContain('档案 2');
  });
  it('IP不匹配仍待核，不取其他账号的IP占用或默认零', async () => {
    const other = {
      ...snapshot,
      ip_groups: snapshot.ip_groups!.map((group) => ({
        ...group,
        owner_account_id: 'other-account',
      })),
    };
    await act(async () => {
      renderer = create(
        <KiloEgressSummary
          accountID="account-a"
          snapshot={other}
          identity={{
            state: 'persisted',
            profiles: [{ ref: 'profile-1', exits: [{ kind: 'ip', ip: '1.1.1.1' }] }],
          }}
        />
      );
    });
    const line = JSON.stringify(
      renderer.root
        .findByType('li')
        .children.map((child) => (typeof child === 'string' ? child : child.props.children))
    );
    expect(text()).toContain('档案关联待核');
    expect(line).toContain('未知');
    expect(text()).not.toContain('档案 1');
  });
  it('直连只匹配direct，不增加公网IP行，并保留陈旧提示', async () => {
    const direct = {
      ...snapshot,
      accounts: [{ account_id: 'account-a', mode: 'direct' as const, primary_proxy_id: '' }],
    };
    await act(async () => {
      renderer = create(
        <KiloEgressSummary accountID="account-a" snapshot={direct} identity={identity} stale />
      );
    });
    expect(text()).toContain('正式出口：直连');
    expect(text()).toContain('档案 2');
    expect(text()).not.toContain('8.8.8.8');
    expect(text()).toContain('快照陈旧');
    expect(renderer.root.findAllByType('li')).toHaveLength(0);
  });
});
