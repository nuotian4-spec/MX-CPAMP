import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PropsWithChildren, ReactNode } from 'react';
import { Button } from '@/components/ui/Button';
import { kiloIPRelations, KiloIPGroups } from './KiloIPRelations';
import type { KiloIPGroup, KiloProxy, KiloProxyState } from '@/services/api/kiloProxies';
vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({ children, footer }: PropsWithChildren<{ footer: ReactNode }>) => (
    <div role="dialog">
      {children}
      {footer}
    </div>
  ),
}));
vi.mock('react-router-dom', () => ({
  Link: ({ children, to }: PropsWithChildren<{ to: string }>) => <a href={to}>{children}</a>,
}));
vi.mock('@/components/ui/DropdownMenu', () => ({
  DropdownMenu: ({ items }: { items: { key: string; label: string; onClick: () => void }[] }) => (
    <>
      {items.map((i) => (
        <Button key={i.key} onClick={i.onClick}>
          {i.label}
        </Button>
      ))}
    </>
  ),
}));
const proxy = (id: string, ip = ''): KiloProxy => ({
  id,
  name: '入口' + id,
  enabled: true,
  order: 0,
  account_id: '',
  scheme: 'http',
  host: '192.0.2.1',
  port: '8080',
  has_auth: true,
  healthy: true,
  active: 0,
  inference_active: 0,
  management_active: 0,
  effective_limit: 10,
  owner: '',
  observed_exit_ip: ip,
  observation_status: ip ? 'ok' : 'unknown',
});
const group: KiloIPGroup = {
  ip: '203.0.113.1',
  owner_account_id: 'a1',
  account_ids: ['a1', 'old'],
  unique_accounts: 2,
  max_unique_accounts: 3,
  draining: false,
  retired: false,
  reason: '',
  state: 'assigned',
  proxy_ids: ['fixed'],
  inference_active: 0,
  management_active: 0,
  capacity: 20,
};
const snapshot: KiloProxyState = {
  revision: 3,
  runtime_revision: 1,
  managed: true,
  proxy_limit: 10,
  connection_ref: 'local',
  ip_groups: [group],
  proxies: [
    { ...proxy('fixed', group.ip), fixed_exit_ip: group.ip },
    proxy('shared', group.ip),
    proxy('unknown'),
  ],
  accounts: [
    { account_id: 'a2', mode: 'pool', candidate_proxy_ids: ['shared'], primary_proxy_id: 'shared' },
  ],
  recycle: [],
};
let renderer: ReactTestRenderer;
const onEdit = vi.fn(),
  onAction = vi.fn(),
  onEgress = vi.fn();
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  vi.unstubAllGlobals();
});
async function render(state = snapshot, range = 'all') {
  await act(async () => {
    renderer = create(
      <KiloIPGroups
        snapshot={state}
        accountLabel={(id) => '账号' + id}
        range={range}
        onEdit={onEdit}
        onAction={onAction}
        onEgress={onEgress}
      />
    );
  });
}
async function click(label: string) {
  await act(async () =>
    renderer.root
      .findAllByType(Button)
      .find((b) => b.props.children === label)!
      .props.onClick()
  );
}
describe('IP关系主表与详情', () => {
  it('同公网IP归组，固定归属以ip_groups为准，未知入口不猜IP', () => {
    const rows = kiloIPRelations(snapshot);
    expect(rows).toHaveLength(2);
    expect(rows[0].entries.map((p) => p.id)).toEqual(['fixed', 'shared']);
    expect(rows[0].group?.owner_account_id).toBe('a1');
    expect(rows[1].ip).toBe('');
  });
  it('主表仅一张，0负载仍保留归属与累计，详情按使用先于入口', async () => {
    await render();
    expect(renderer.root.findAllByType('table')).toHaveLength(1);
    expect(JSON.stringify(renderer.toJSON())).toContain('归属保留 · 当前空闲');
    expect(JSON.stringify(renderer.toJSON())).toContain('2 / 3');
    await click('详情');
    const headers = renderer.root.findAllByType('h3').map((h) => h.props.children);
    expect(headers.slice(0, 2)).toEqual(['归属与容量', '正在使用']);
    expect(headers[3]).toBe('固定池累计分配历史');
    expect(JSON.stringify(renderer.toJSON())).toContain('分配时间未知');
    expect(
      renderer.root.findAllByType('a').some((a) => a.props.href === '/kilo-accounts?account=old')
    ).toBe(true);
  });
  it('详情保留候选/主偏好与管理动作，账号出口沿原入口', async () => {
    await render();
    await click('详情');
    expect(JSON.stringify(renderer.toJSON())).toContain('指定候选：');
    expect(
      renderer.root.findAllByType('a').some((a) => a.props.href === '/kilo-accounts?account=a2')
    ).toBe(true);
    await click('编辑代理');
    expect(onEdit).toHaveBeenCalledWith(snapshot.proxies[0]);
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
    await click('详情');
    const edit = renderer.root
      .findAllByType(Button)
      .find((b) => Array.isArray(b.props.children) && b.props.children[0] === '配置 ')!;
    await act(async () => edit.props.onClick());
    expect(onEgress).toHaveBeenCalledWith('a1');
  });
  it('共享入口不冒充固定池累计或统一限额', async () => {
    await render({ ...snapshot, ip_groups: [], proxies: [proxy('one', '203.0.113.9')] });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('实测公网出口');
    expect(text).toContain('IP统一上限未知');
    expect(text).toContain('共享模式不套用固定池累计上限');
  });
  it('固定与共享同IP只标固定池累计，保留共享累计未知', async () => {
    await render();
    const table = JSON.stringify(renderer.toJSON());
    expect(table).toContain('固定池 2 / 3');
    expect(table).toContain('共享入口累计未知');
    await click('详情');
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('固定池累计分配历史');
    expect(text).toContain('共享入口累计账号历史未知');
  });
  it('固定IP缺少权威group时归属和累计均待核，不归为共享池', async () => {
    await render({
      ...snapshot,
      ip_groups: [],
      proxies: [{ ...proxy('fixed'), fixed_exit_ip: group.ip }],
    });
    await click('详情');
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('固定IP归属待核');
    expect(text).toContain('未知 · 固定池累计待核');
    expect(text).toContain('固定IP累计账号历史待核');
    expect(text).not.toContain('共享池');
    expect(text).not.toContain('共享模式');
  });
  it('未命名待识别入口以脱敏地址区分，不宣称同IP归组', async () => {
    await render({ ...snapshot, ip_groups: [], proxies: [{ ...proxy('unknown'), name: '' }] });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('http://192.0.2.1:8080');
    expect(text).toContain('独立入口 · 公网IP待识别');
    expect(text).not.toContain('同 IP 归组');
    expect(text).not.toContain('@');
  });
  it('待识别与归档筛选分别呈现，历史归档不清账号集合', async () => {
    await render(snapshot, 'unknown');
    expect(renderer.root.findAllByType('tbody')[0].findAllByType('tr')).toHaveLength(1);
    expect(JSON.stringify(renderer.toJSON())).toContain('公网 IP 未知');
    await act(async () => renderer.unmount());
    await render(
      {
        ...snapshot,
        proxies: [],
        ip_groups: [{ ...group, retired: true, state: 'retired', owner_account_id: '' }],
      },
      'archived'
    );
    expect(JSON.stringify(renderer.toJSON())).toContain('已退休');
    await click('详情');
    expect(JSON.stringify(renderer.toJSON())).toContain('账号old');
  });
});

it('待核按条目计且不计可分配IP，登记与当前观测分开展示', async () => {
  const pending: KiloProxy = {
    ...proxy('pending', '203.0.113.22'),
    admission_status: 'pending',
    admission_reason: 'ip_changed',
    admitted_ip: '203.0.113.11',
    admitted_at: '2026-11-15T00:00:00Z',
  };
  const state = { ...snapshot, ip_groups: [], proxies: [pending] };
  expect(kiloIPRelations(state)[0].ip).toBe('203.0.113.11');
  await render(state);
  await click('详情');
  const text = JSON.stringify(renderer.toJSON());
  expect(text).toContain('已保存待核');
  expect(text).toContain('待核不贡献容量');
  expect(text).toContain('登记IP：');
  expect(text).toContain('当前观测IP：');
  expect(text).toContain('与登记IP不同');
  expect(renderer.root.findAllByType('table')).toHaveLength(1);
  const summary = renderer.root
    .findAllByType('span')
    .find((x) => x.children.includes('当前可分配 IP'))!.parent!;
  expect(summary.findByType('b').children).toEqual(['0']);
});
it('待核筛选保留待核行，旧版兼容不冒充已登记', async () => {
  const state = {
    ...snapshot,
    ip_groups: [],
    proxies: [
      {
        ...proxy('pending'),
        admission_status: 'pending' as const,
        admission_reason: 'unverified' as const,
      },
      proxy('legacy', '203.0.113.8'),
    ],
  };
  await act(async () => {
    renderer = create(<KiloIPGroups snapshot={state} accountLabel={(id) => id} filter="pending" />);
  });
  expect(JSON.stringify(renderer.toJSON())).toContain('入口pending');
  expect(JSON.stringify(renderer.toJSON())).not.toContain('203.0.113.8');
});

const trustedGroupPending: KiloProxyState = {
  ...snapshot,
  ip_groups: [{ ...group, state: 'unverified', capacity: 0 }],
  proxies: [
    {
      ...proxy('fixed'),
      fixed_exit_ip: group.ip,
      admission_status: 'pending',
      admission_reason: 'probe_failed',
      admitted_ip: '',
      admitted_at: null,
      observed_exit_ip: null,
      observed_at: null,
      observation_status: 'failed',
      healthy: false,
    },
  ],
};

it('旧固定组首次观测失败仍保留可信成员关系，不拆待识别重复行', () => {
  const rows = kiloIPRelations(trustedGroupPending);
  expect(rows).toHaveLength(1);
  expect(rows[0].ip).toBe(group.ip);
  expect(rows[0].group?.proxy_ids).toEqual(['fixed']);
  expect(rows[0].entries).toEqual(trustedGroupPending.proxies);
});

it('已识别范围保留旧固定组待核入口与详情管理动作，仍不贡献容量', async () => {
  await render(trustedGroupPending, 'known');
  await click('详情');
  const dialog = renderer.root.findByProps({ role: 'dialog' });
  expect(dialog.findAllByType('strong').some((node) => node.props.children === '入口fixed')).toBe(
    true
  );
  expect(JSON.stringify(renderer.toJSON())).toContain('待核不贡献容量');
  expect(JSON.stringify(renderer.toJSON())).toContain('已保存待核');
  expect(
    dialog.findAllByType('a').some((node) => node.props.href === '/kilo-accounts?account=old')
  ).toBe(true);
  const capacity = renderer.root
    .findAllByType('span')
    .find((node) => node.children.includes('当前可分配 IP'))!.parent!;
  expect(capacity.findByType('b').children).toEqual(['0']);
  await click('健康与公网IP重验');
  expect(onAction).toHaveBeenCalledWith({
    kind: 'recheck',
    proxy: trustedGroupPending.proxies[0],
  });
});

it.each(['无固定IP', '仅固定IP'])('无可信组新待核入口%s仍待识别且不贡献容量', async (variant) => {
  const pending: KiloProxy = {
    ...proxy('new-pending', group.ip),
    admission_status: 'pending',
    admission_reason: 'unverified',
    admitted_ip: '',
    admitted_at: null,
    fixed_exit_ip: variant === '仅固定IP' ? group.ip : '',
  };
  const state = { ...snapshot, ip_groups: [], proxies: [pending] };
  const rows = kiloIPRelations(state);
  expect(rows).toHaveLength(1);
  expect(rows[0].ip).toBe('');
  expect(rows[0].key).toBe('unknown:new-pending');
  await render(state, 'unknown');
  expect(JSON.stringify(renderer.toJSON())).toContain('入口new-pending');
  await click('详情');
  expect(JSON.stringify(renderer.toJSON())).toContain('待核不贡献容量');
  const capacity = renderer.root
    .findAllByType('span')
    .find((node) => node.children.includes('当前可分配 IP'))!.parent!;
  expect(capacity.findByType('b').children).toEqual(['0']);
});
