import { KiloAccountEgress } from './KiloAccountEgress';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PropsWithChildren } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloProxiesPanel } from './KiloProxiesPage';
import type { KiloProxyState } from '@/services/api/kiloProxies';
const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  capacity: vi.fn(),
  egress: vi.fn(),
  accounts: vi.fn(),
  archive: vi.fn(),
  restore: vi.fn(),
  clear: vi.fn(),
  recheck: vi.fn(),
}));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: mocks,
}));
vi.mock('@/services/api/kilo', () => ({ kiloApi: { list: mocks.accounts } }));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: PropsWithChildren) => <div role="dialog">{children}</div>,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'ADMIN_FIXTURE' };
const account = {
  id: 'a1',
  name: '账号一',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
};
const snapshot: KiloProxyState = {
  revision: 7,
  runtime_revision: 7,
  managed: true,
  proxy_limit: 10,
  connection_ref: 'a'.repeat(32),
  accounts: [{ account_id: 'a1', mode: 'pool', primary_proxy_id: 'p1' }],
  recycle: [
    {
      id: 'r1',
      name: '归档代理一',
      order: 3,
      enabled: false,
      original_account_id: 'a1',
      scheme: 'http',
      host: '127.0.0.3',
      port: '8081',
      has_auth: true,
      archived_at: '2026-11-13T01:02:03Z',
    },
  ],
  proxies: [
    {
      id: 'p1',
      name: '代理一',
      order: 0,
      enabled: true,
      account_id: 'a1',
      scheme: 'http',
      host: '127.0.0.1',
      port: '8080',
      has_auth: true,
      healthy: false,
      active: 1,
      owner: 'a1',
    },
    {
      id: 'p2',
      name: '他人专属',
      order: 1,
      enabled: true,
      account_id: 'a2',
      scheme: 'socks5',
      host: '127.0.0.2',
      port: '1080',
      has_auth: false,
      healthy: true,
      active: 0,
      owner: '',
    },
  ],
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { resolve, promise };
}
describe('Kilo代理与出口页面', () => {
  let renderer: ReactTestRenderer;
  const storage = { setItem: vi.fn() };
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('sessionStorage', storage);
    mocks.list.mockResolvedValue(snapshot);
    mocks.accounts.mockResolvedValue({
      accounts: [account],
      connection_ref: snapshot.connection_ref,
    });
    for (const method of [
      mocks.create,
      mocks.update,
      mocks.capacity,
      mocks.egress,
      mocks.archive,
      mocks.restore,
      mocks.clear,
      mocks.recheck,
    ])
      method.mockResolvedValue({ ...snapshot, revision: 8, runtime_revision: 8 });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer.unmount());
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(<KiloProxiesPanel scope={scope} />);
    });
    if (renderer.root.findAllByType(Select).some((item) => item.props.ariaLabel === '出口范围'))
      await select('出口范围', 'all');
  }
  async function renderEgress() {
    await act(async () => {
      renderer = create(
        <KiloAccountEgress
          account={account}
          scope={scope}
          connectionRef={snapshot.connection_ref}
        />
      );
    });
  }
  async function recycleTab() {
    await act(async () =>
      renderer.root
        .findAllByType(Button)
        .find((b) => b.props.role === 'tab' && JSON.stringify(b.props.children).includes('回收站'))!
        .props.onClick()
    );
  }
  const text = () => JSON.stringify(renderer.toJSON());
  const button = (label: string) =>
    renderer.root.findAllByType(Button).filter((b) => b.props.children === label);
  async function click(label: string) {
    if (['归档代理', '健康与公网IP重验'].includes(label) && button(label).length === 0)
      await act(async () =>
        renderer.root
          .findAllByType(Button)
          .find((b) => b.props.role === 'tab' && b.props.children === '出口池')!
          .props.onClick()
      );
    if (['恢复代理', '清空回收站'].includes(label) && button(label).length === 0)
      await recycleTab();
    if (['编辑代理', '归档代理', '健康与公网IP重验'].includes(label) && button(label).length === 0)
      await click('详情');
    const matches = button(label);
    expect(matches.length).toBeGreaterThan(0);
    await act(async () => {
      matches[matches.length - 1].props.onClick();
    });
  }
  async function input(label: string, value: string) {
    await act(async () => {
      renderer.root
        .findAllByType(Input)
        .find((i) => i.props.label === label)!
        .props.onChange({ target: { value } });
    });
  }
  async function select(label: string, value: string) {
    await act(async () => {
      renderer.root
        .findAllByType(Select)
        .find((i) => i.props.ariaLabel === label)!
        .props.onChange(value);
    });
  }
  async function submit() {
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
  }
  it.each([
    ['pending', '代理已保存但未入池，不增加容量'],
    ['admitted', '代理已保存并登记公网IP'],
    ['legacy', '旧版兼容，准入状态未知'],
  ] as const)('单条新增按准入状态%s报告结果', async (admission, message) => {
    mocks.create.mockResolvedValue({
      ...snapshot,
      revision: 8,
      runtime_revision: 8,
      proxies: [
        ...snapshot.proxies,
        {
          ...snapshot.proxies[0],
          id: 'new-proxy',
          account_id: '',
          active: 0,
          owner: '',
          admission_status: admission,
          admission_reason: admission === 'pending' ? 'probe_failed' : '',
          admitted_ip: admission === 'admitted' ? '203.0.113.10' : '',
        },
      ],
    });
    await render();
    await click('增加代理');
    await input('代理URL', 'http://USER:SECRET_NEW@127.0.0.1:8080');
    await submit();
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(text()).toContain(message);
    expect(text()).not.toContain('SECRET_NEW');
    expect(button('增加代理')[0].props.disabled).toBe(false);
  });
  it('单条明确重复定位原记录，不误报成功或要求重放', async () => {
    mocks.create.mockRejectedValue({
      status: 409,
      data: { error: 'proxy_ip_already_registered', ip: '203.0.113.10', proxy_id: 'p1' },
    });
    await render();
    await click('增加代理');
    await input('代理URL', 'http://USER:SECRET_DUPLICATE@127.0.0.1:8080');
    await submit();
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(text()).toContain('公网IP已登记');
    expect(text()).not.toContain('代理配置已保存');
    expect(text()).not.toContain('SECRET_DUPLICATE');
    expect(button('增加代理')[0].props.disabled).toBe(false);
    expect(
      renderer.root
        .findByProps({ role: 'dialog' })
        .findAllByType('strong')
        .some((node) => node.props.children === '代理一')
    ).toBe(true);
  });
  it('共享代理展示指定候选账号链接，实际占用保持独立列', async () => {
    mocks.accounts.mockResolvedValue({
      accounts: [account, { ...account, id: 'auto', name: '自动共享账号' }],
      connection_ref: snapshot.connection_ref,
    });
    mocks.list.mockResolvedValue({
      ...snapshot,
      accounts: [
        { account_id: 'a1', mode: 'pool', candidate_proxy_ids: ['shared'], primary_proxy_id: '' },
        { account_id: 'auto', mode: 'pool', primary_proxy_id: '' },
      ],
      proxies: [
        {
          ...snapshot.proxies[0],
          id: 'shared',
          name: '共享备用出口',
          account_id: '',
          active: 0,
          owner: '',
        },
      ],
    });
    await render();
    expect(renderer.root.findAllByType('table')).toHaveLength(1);
    await click('详情');
    const links = renderer.root
      .findByProps({ role: 'dialog' })
      .findAllByType('a')
      .map((node) => node.props.href);
    expect(links).toContain('/kilo-accounts?account=a1');
    expect(links).not.toContain('/kilo-accounts?account=auto');
    expect(text()).toContain('当前没有在途请求');
    expect(text()).toContain('指定候选：');
    expect(text()).toContain('共享池');
    expect(text()).not.toContain('满载直连');
    expect(text()).not.toContain('满载fallback');
  });
  it('轮询发现新连接时清旧代理账号和编辑草稿', async () => {
    vi.useFakeTimers();
    try {
      await render();
      await click('编辑代理');
      expect(renderer.root.findAllByType('form')).toHaveLength(1);
      mocks.list.mockResolvedValue({ ...snapshot, connection_ref: 'b'.repeat(32) });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });
      expect(renderer.root.findAllByType('form')).toHaveLength(0);
      expect(text()).not.toContain('代理一');
      expect(text()).toContain('管理连接已变化');
    } finally {
      vi.useRealTimers();
    }
  });
  it('直连账号出口在详情呈现，代理页不再保留账号大表', async () => {
    mocks.list.mockResolvedValue({
      ...snapshot,
      accounts: [{ account_id: 'a1', mode: 'direct', primary_proxy_id: '' }],
    });
    await render();
    expect(text()).not.toContain('账号出口');
    await act(async () => renderer.unmount());
    await renderEgress();
    expect(text()).toContain('出口：直连');
    expect(text()).not.toContain('按共享池分配');
  });
  it('自动归属active为0显示已绑定空闲，不混作共享空闲', async () => {
    mocks.list.mockResolvedValue({
      ...snapshot,
      proxies: [
        {
          ...snapshot.proxies[0],
          account_id: '',
          active: 0,
          owner: '',
          fixed_exit_ip: '203.0.113.10',
        },
      ],
      ip_groups: [
        {
          ip: '203.0.113.10',
          owner_account_id: 'a1',
          account_ids: ['a1'],
          unique_accounts: 1,
          max_unique_accounts: 2,
          proxy_ids: ['p1'],
          state: 'assigned',
          draining: false,
          retired: false,
          reason: '',
        },
      ],
    });
    await render();
    expect(text()).toContain('已独占');
    expect(text()).toContain('未知');
    expect(
      renderer.root
        .findAllByType('a')
        .some((link) => link.props.href === '/kilo-accounts?account=a1')
    ).toBe(true);
  });
  it('固定IP恢复按自动池规则说明并保留累计历史', async () => {
    const fixed = {
      ...snapshot.recycle[0],
      original_account_id: '',
      fixed_exit_ip: '203.0.113.10',
      retirement_reason: 'unique_account_limit' as const,
    };
    mocks.list.mockResolvedValue({ ...snapshot, recycle: [fixed] });
    mocks.restore.mockResolvedValue({
      ...snapshot,
      revision: 8,
      recycle: [],
      proxies: [
        {
          ...snapshot.proxies[0],
          fixed_exit_ip: '203.0.113.10',
          account_id: '',
          active: 0,
          owner: '',
        },
      ],
    });
    await render();
    await recycleTab();
    expect(text()).toContain('普通代理通过后回到共享池');
    expect(text()).toContain('固定IP保留累计历史');
    expect(text()).toContain('自动独占池（历史见IP归组）');
    expect(text()).not.toContain('原共享');
    await click('恢复代理');
    expect(text()).toContain('恢复到自动独占池');
    const dialog = renderer.root.findByProps({ role: 'dialog' });
    expect(
      JSON.stringify(dialog.findAllByType('p').map((item) => item.props.children))
    ).not.toContain('恢复到共享池');
    expect(text()).toContain('累计不同账号数低于N');
    await click('检测并恢复');
    expect(text()).toContain('固定IP已恢复到自动独占池；累计历史保留');
    expect(text()).not.toContain('代理已启用并恢复到共享池');
    expect(text()).toContain('回收站为空');
  });
  it('展示同投影版本、真实健康/占用与归档白名单', async () => {
    await render();
    expect(text()).toContain('运行快照');
    expect(text()).toContain('未健康');
    await click('详情');
    expect(
      renderer.root
        .findAllByType('a')
        .some((node) => node.props.href === '/kilo-accounts?account=a1')
    ).toBe(true);
    expect(text()).toContain('回收站');
    await recycleTab();
    expect(text()).toContain('原专属：账号一');
    expect(text()).toContain('2026-11-13T01:02:03Z');
    expect(text()).not.toContain('删除');
  });
  it('增加URL提交立即清空、防重复；未持久化到浏览器', async () => {
    const pending = deferred<KiloProxyState>();
    mocks.create.mockReturnValue(pending.promise);
    await render();
    await click('增加代理');
    await input('代理URL', 'http://user:SECRET@127.0.0.1:8080');
    let saving!: Promise<void>;
    await act(async () => {
      saving = renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.type === 'password')!.props.value
    ).toBe('');
    await submit();
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.create.mock.calls[0][1].revision).toBe(7);
    await act(async () => {
      pending.resolve({ ...snapshot, revision: 8 });
      await saving;
    });
    expect(text()).not.toContain('SECRET');
    expect(storage.setItem).not.toHaveBeenCalled();
  });
  it('编辑只改实际字段，不从host投影拼回URL；可手动换URL', async () => {
    await render();
    await click('编辑代理');
    await input('代理名称', '新代理名');
    await input('代理顺序', '4');
    await select('专属归属', '');
    await act(async () =>
      renderer.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label === '启用代理')!
        .props.onChange(false)
    );
    await submit();
    expect(mocks.update.mock.calls[0][1]).toEqual({
      name: '新代理名',
      order: 4,
      account_id: '',
      enabled: false,
    });
    await click('编辑代理');
    await input('替换代理URL（留空保留）', 'socks5://u:SECRET_REPLACE@127.0.0.1:1080');
    await submit();
    expect(mocks.update.mock.calls[1][1].url).toContain('SECRET_REPLACE');
    expect(text()).not.toContain('SECRET_REPLACE');
  });
  it('取消清URL；容量1至100且带当前revision', async () => {
    await render();
    await click('增加代理');
    await input('代理URL', 'SECRET_CANCEL');
    await click('取消');
    await click('增加代理');
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.type === 'password')!.props.value
    ).toBe('');
    await click('取消');
    await click('调整容量');
    await input('每代理并发容量', '101');
    await submit();
    expect(mocks.capacity).not.toHaveBeenCalled();
    await input('每代理并发容量', '12');
    await submit();
    expect(mocks.capacity.mock.calls[0][0]).toBe(12);
    expect(mocks.capacity.mock.calls[0][1].revision).toBe(7);
  });
  it('明确direct全量提交空专属/主偏好，不改变满载fallback', async () => {
    await renderEgress();
    await click('调整出口配置');
    expect(
      renderer.root.findAllByType('input').filter((i) => i.props.type === 'checkbox')
    ).toHaveLength(0);
    await select('出口策略', 'direct');
    expect(text()).toContain('释放本账号专属出口');
    expect(mocks.egress).not.toHaveBeenCalled();
    await submit();
    expect(mocks.egress.mock.calls[0][1]).toEqual({
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
  it('pool全量专属与主偏好，排除他人专属', async () => {
    await renderEgress();
    await click('调整出口配置');
    const choices = renderer.root.findAllByType(Select).find((i) => i.props.ariaLabel === '主偏好')!
      .props.options;
    expect(choices.map((i: { value: string }) => i.value)).toEqual(['', 'p1']);
    await submit();
    expect(mocks.egress.mock.calls[0][1]).toEqual({
      capacity_mode: 'fixed',
      allocation_mode: 'auto',
      max_ips: 1,
      specified_ips: [],
      mode: 'pool',
      candidate_proxy_ids: null,
      exclusive_proxy_ids: ['p1'],
      primary_proxy_id: 'p1',
    });
  });
  it('409关闭表单要求刷新，再次显式提交才发请求', async () => {
    mocks.capacity.mockRejectedValueOnce({ status: 409, message: 'SECRET_SERVER' });
    await render();
    await click('调整容量');
    await submit();
    expect(text()).toContain('显式提交');
    expect(text()).not.toContain('SECRET_SERVER');
    expect(button('调整容量')[0].props.disabled).toBeTruthy();
    expect(mocks.list).toHaveBeenCalledTimes(1);
    mocks.list.mockResolvedValue({ ...snapshot, revision: 9, runtime_revision: 9 });
    await click('刷新状态');
    expect(mocks.capacity).toHaveBeenCalledTimes(1);
    await click('调整容量');
    await submit();
    expect(mocks.capacity).toHaveBeenCalledTimes(2);
    expect(mocks.capacity.mock.calls[1][1].revision).toBe(9);
  });
  it('连接切换清URL，迟到写响应不覆盖新连接并取消旧请求', async () => {
    const pending = deferred<KiloProxyState>();
    mocks.create.mockReturnValue(pending.promise);
    await render();
    await click('增加代理');
    await input('代理URL', 'SECRET_OLD');
    let saving!: Promise<void>;
    await act(async () => {
      saving = renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    const oldSignal = mocks.create.mock.calls[0][3] as AbortSignal;
    const nextScope = { ...scope, apiBase: 'https://next.invalid' };
    const next = { ...snapshot, connection_ref: 'b'.repeat(32), proxies: [] };
    mocks.list.mockResolvedValue(next);
    mocks.accounts.mockResolvedValue({ accounts: [], connection_ref: next.connection_ref });
    await act(async () => renderer.update(<KiloProxiesPanel scope={nextScope} />));
    await act(async () => {
      pending.resolve({ ...snapshot, revision: 8 });
      await saving;
    });
    expect(oldSignal.aborted).toBe(true);
    expect(text()).toContain('暂无符合条件的出口');
    expect(text()).not.toContain('SECRET_OLD');
    expect(text()).not.toContain('代理配置已保存');
  });
  it('跨连接账号/ref不混用；加载失败不伪报空列表', async () => {
    mocks.accounts.mockResolvedValue({ accounts: [account], connection_ref: 'b'.repeat(32) });
    await render();
    expect(text()).toContain('读取失败');
    expect(text()).not.toContain('尚无代理');
    expect(button('增加代理')[0].props.disabled).toBeTruthy();
  });
  it('四操作确认前取消均不发请求，归档确认后使用当前版本', async () => {
    await render();
    for (const label of ['归档代理', '恢复代理', '清空回收站', '健康与公网IP重验']) {
      await click(label);
      await click('取消');
    }
    for (const method of [mocks.archive, mocks.restore, mocks.clear, mocks.recheck])
      expect(method).not.toHaveBeenCalled();
    mocks.archive.mockResolvedValue({ ...snapshot, revision: 8, proxies: [snapshot.proxies[0]] });
    await click('归档代理');
    expect(text()).toContain('相关主偏好与出口关联会清除');
    await click('确认归档');
    expect(mocks.archive).toHaveBeenCalledTimes(1);
    expect(mocks.archive.mock.calls[0].slice(0, 3)).toEqual(['p2', snapshot, scope]);
    expect(text()).toContain('代理已归档');
  });
  it('恢复强制检测成功进入共享池，不回填原专属或主偏好', async () => {
    const restored = {
      ...snapshot.proxies[0],
      id: 'r1',
      name: '归档代理一',
      enabled: true,
      account_id: '',
      active: 0,
      owner: '',
      healthy: true,
    };
    mocks.restore.mockResolvedValue({
      ...snapshot,
      revision: 8,
      runtime_revision: 8,
      recycle: [],
      proxies: [restored],
      accounts: [{ account_id: 'a1', mode: 'pool', primary_proxy_id: '' }],
    });
    await render();
    await click('恢复代理');
    expect(text()).toContain('最长10秒');
    expect(text()).toContain('旧专属关系、占用和连接均不恢复');
    await click('检测并恢复');
    expect(mocks.restore.mock.calls[0].slice(0, 3)).toEqual(['r1', snapshot, scope]);
    expect(text()).toContain('已启用并恢复到共享池');
    expect(text()).toContain('回收站为空');
    expect(text()).not.toContain('原专属：');
    await click('出口池');
    expect(text()).toContain('共享池');
  });
  it.each([499, 502, 409])('恢复失败%s保留站内旧记录，关闭操作且不自动再试', async (status) => {
    mocks.restore.mockRejectedValue({ status, message: 'SECRET_FAILURE' });
    await render();
    await click('恢复代理');
    await click('检测并恢复');
    expect(text()).toContain('归档代理一');
    expect(text()).not.toContain('已启用并恢复到共享池');
    expect(text()).not.toContain('SECRET_FAILURE');
    expect(button('恢复代理')[0].props.disabled).toBe(true);
    expect(mocks.restore).toHaveBeenCalledTimes(1);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    if (status === 499) expect(text()).toContain('请求已取消');
    await click('刷新状态');
    expect(mocks.restore).toHaveBeenCalledTimes(1);
  });
  it('等待中写互斥，取消请求后迟到成功不改写状态或报成功', async () => {
    const pending = deferred<KiloProxyState>();
    mocks.restore.mockReturnValue(pending.promise);
    await render();
    await click('恢复代理');
    await click('检测并恢复');
    const signal = mocks.restore.mock.calls[0][3] as AbortSignal;
    expect(button('正在检测并恢复')[0].props.disabled).toBe(true);
    expect(button('增加代理')[0].props.disabled).toBe(true);
    await click('取消请求');
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve({ ...snapshot, recycle: [], revision: 8 }));
    expect(text()).toContain('请求已取消');
    expect(text()).toContain('归档代理一');
    expect(text()).not.toContain('已启用并恢复到共享池');
    expect(mocks.restore).toHaveBeenCalledTimes(1);
  });
  it('已停用代理强制重验不启用、不改配置版本、不当作模型验证', async () => {
    const disabled = { ...snapshot, proxies: [{ ...snapshot.proxies[0], enabled: false }] };
    mocks.list.mockResolvedValue(disabled);
    mocks.recheck.mockResolvedValue({
      ...disabled,
      proxies: [{ ...disabled.proxies[0], healthy: false }],
    });
    await render();
    await click('健康与公网IP重验');
    expect(text()).toContain('不启用已停用代理');
    expect(text()).toContain('不使用健康缓存');
    await click('开始重验');
    expect(mocks.recheck.mock.calls[0].slice(0, 3)).toEqual(['p1', disabled, scope]);
    await click('详情');
    expect(text()).toContain('已停用');
    expect(text()).toContain('未健康');
    expect(text()).toContain('不是模型可用性验证');
    expect(text()).not.toContain('代理配置已保存');
    expect(mocks.recheck.mock.calls[0][1].revision).toBe(7);
  });
  it('清空仅删除站内记录，活跃代理池仍在', async () => {
    mocks.clear.mockResolvedValue({ ...snapshot, revision: 8, recycle: [] });
    await render();
    await click('清空回收站');
    expect(text()).toContain('仅永久清除回收站记录，不影响活跃代理池');
    await click('确认清空');
    expect(mocks.clear.mock.calls[0].slice(0, 2)).toEqual([snapshot, scope]);
    expect(text()).toContain('回收站为空');
    expect(button('清空回收站')[0].props.disabled).toBe(true);
    await click('出口池');
    expect(text()).toContain('他人专属');
  });
  it('连接切换中止恢复，迟到结果不污染新连接回收站', async () => {
    const pending = deferred<KiloProxyState>();
    mocks.restore.mockReturnValue(pending.promise);
    await render();
    await click('恢复代理');
    await click('检测并恢复');
    const signal = mocks.restore.mock.calls[0][3] as AbortSignal;
    const nextScope = { ...scope, apiBase: 'https://next.invalid' };
    const next = { ...snapshot, connection_ref: 'b'.repeat(32), recycle: [], proxies: [] };
    mocks.list.mockResolvedValue(next);
    mocks.accounts.mockResolvedValue({ accounts: [], connection_ref: next.connection_ref });
    await act(async () => renderer.update(<KiloProxiesPanel scope={nextScope} />));
    await act(async () => pending.resolve({ ...snapshot, revision: 8 }));
    expect(signal.aborted).toBe(true);
    expect(text()).toContain('回收站为空');
    expect(text()).not.toContain('归档代理一');
    expect(text()).not.toContain('已启用并恢复到共享池');
  });
});

vi.mock('@/components/ui/DropdownMenu', () => ({
  DropdownMenu: ({
    items,
    disabled,
  }: {
    items: import('@/components/ui/DropdownMenu').DropdownMenuItem[];
    disabled?: boolean;
  }) => (
    <>
      {items.map((item) =>
        item.type === 'divider' ? null : (
          <Button
            key={item.key}
            disabled={Boolean(disabled || item.disabled)}
            onClick={item.onClick}
          >
            {item.label}
          </Button>
        )
      )}
    </>
  ),
}));

vi.mock('react-router-dom', async (original) => ({
  ...(await original<typeof import('react-router-dom')>()),
  Link: ({ children, to }: PropsWithChildren<{ to: string }>) => <a href={to}>{children}</a>,
}));

vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({
    children,
    footer,
  }: import('react').PropsWithChildren<{ footer?: import('react').ReactNode }>) => (
    <div role="dialog">
      {children}
      {footer}
    </div>
  ),
}));
