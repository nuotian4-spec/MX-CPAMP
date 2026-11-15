import { KiloQuotaSummary } from './KiloQuotaSummary';
import type { CredentialConcurrencyState } from '@/features/accounts/hooks/useCredentialConcurrencySnapshot';
import type { KiloQuotaView, KiloSourcePolicySnapshot } from '@/services/api/kiloQuota';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PropsWithChildren } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { Select } from '@/components/ui/Select';
import { KiloAccountsPanel } from './KiloAccountsPage';
import type { KiloAccount, KiloAccountsSnapshot, KiloOrganizations } from '@/services/api/kilo';
import { KiloDeviceAuth } from './KiloDeviceAuth';
import { KiloQuotaPanel } from './KiloQuotaPanel';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  organizations: vi.fn(),
  proxyList: vi.fn(),
  quotaGet: vi.fn(),
  quotaQuery: vi.fn(),
  policyGet: vi.fn(),
}));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: { get: mocks.quotaGet, query: mocks.quotaQuery, getPolicy: mocks.policyGet },
}));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: mocks,
}));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: { list: mocks.proxyList },
}));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('react-router-dom', async (original) => ({
  ...(await original<typeof import('react-router-dom')>()),
  Link: ({ children, to }: PropsWithChildren<{ to: string }>) => <a href={to}>{children}</a>,
}));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: PropsWithChildren) => <div role="dialog">{children}</div>,
}));
vi.mock('./KiloDeviceAuth', () => ({ KiloDeviceAuth: () => <div>设备授权面板</div> }));
vi.mock('./KiloQuotaPanel', () => ({ KiloQuotaPanel: () => <div>额度面板</div> }));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'ADMIN_FIXTURE' };
const reference = 'a'.repeat(32);
const account: KiloAccount = {
  id: 'kilo-fixture.json',
  name: '合成账号',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 2,
  waiting: 1,
  allow_direct_when_proxy_full: false,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
describe('Kilo账号页', () => {
  let renderer: ReactTestRenderer;
  const storage = { setItem: vi.fn() };
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.policyGet.mockReset();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('sessionStorage', storage);
    mocks.proxyList.mockResolvedValue({
      revision: 1,
      runtime_revision: 1,
      managed: true,
      proxy_limit: 10,
      connection_ref: reference,
      accounts: [],
      proxies: [],
      recycle: [],
    });
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: reference });
    mocks.create.mockResolvedValue({ ...account, connection_ref: reference });
    mocks.update.mockResolvedValue({ ...account, connection_ref: reference });
    mocks.remove.mockResolvedValue(undefined);
    mocks.organizations.mockResolvedValue({
      organizations: [{ id: 'org_first', name: '第一组织' }],
      has_personal_account: true,
      connection_ref: reference,
    });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer.unmount());
    vi.unstubAllGlobals();
  });
  async function render(nextScope = scope) {
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={nextScope} />);
    });
  }
  async function click(label: string) {
    const matches = renderer.root
      .findAllByType(Button)
      .filter((item) => item.props.children === label);
    expect(matches.length).toBeGreaterThan(0);
    await act(async () => {
      matches[matches.length - 1].props.onClick();
    });
  }
  async function input(label: string, value: string) {
    await act(async () => {
      renderer.root
        .findAllByType(Input)
        .find((item) => item.props.label === label)!
        .props.onChange({ target: { value } });
    });
  }
  async function submit() {
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
  }
  function text() {
    return JSON.stringify(renderer.toJSON());
  }
  const sourceView: KiloQuotaView = {
    account_id: account.id,
    binding_revision: 2,
    connection_ref: reference,
    source: { kind: 'cpa_codex', credential_id: 'codex-source', status: 'ready' },
    query: { status: 'success', attempted_at: null, observed_at: null, error_code: null },
    plan: null,
    windows: [],
    credits: { remaining: null, source_field: null, unit: null },
    stale: false,
  };
  const readyPolicy: KiloSourcePolicySnapshot = {
    enabled: true,
    interval_seconds: 1800,
    revision: 1,
    paused: false,
    pause_generation: 0,
    scope: '18000+604800',
    status: 'ready',
    observed_at: null,
    next_query_at: null,
    error_code: null,
    persisted: true,
    connection_ref: reference,
  };
  function setupQuotaAccount() {
    mocks.list.mockResolvedValue({
      connection_ref: reference,
      accounts: [
        {
          ...account,
          source_policy: readyPolicy,
          quota_snapshot: sourceView,
          quota_source_summary: {
            ...sourceView.source,
            display_label: '关联Codex',
            binding_revision: 2,
          },
        },
      ],
    });
    mocks.quotaGet.mockResolvedValue(sourceView);
    mocks.quotaQuery.mockResolvedValue({
      view: {
        ...sourceView,
        windows: [
          {
            group: 'rate_limit',
            slot: 'primary_window',
            duration_seconds: 18000,
            remaining_percent: 0,
            used_percent: 100,
            reset_at: null,
            source: 'api_query',
          },
        ],
      },
      cancelled: false,
    });
    mocks.policyGet.mockResolvedValue({
      ...readyPolicy,
      status: 'paused',
      paused: true,
      pause_generation: 1,
    });
  }
  it('批量额度查询完成后立即读取并发布来源暂停，不等待手动刷新或轮询', async () => {
    setupQuotaAccount();
    await render();
    expect(text()).toContain('来源策略当前未暂停');
    await click('查询已绑定账号额度');
    expect(mocks.quotaQuery).toHaveBeenCalledTimes(1);
    expect(mocks.policyGet).toHaveBeenCalledWith(account.id, scope, expect.any(AbortSignal));
    expect(text()).toContain('来源额度策略停调');
    expect(text()).not.toContain('来源策略当前未暂停');
    expect(mocks.list).toHaveBeenCalledTimes(1);
    expect(
      renderer.root.findByType(KiloQuotaSummary).props.account.quota_snapshot.windows[0]
        .used_percent
    ).toBe(100);
  });
  it.each(['failure', 'wrong-ref'])(
    '策略重读%s保留已取得额度，来源策略显示未知且不重放查询',
    async (mode) => {
      setupQuotaAccount();
      if (mode === 'failure') mocks.policyGet.mockRejectedValue({ status: 503 });
      else mocks.policyGet.mockResolvedValue({ ...readyPolicy, connection_ref: 'b'.repeat(32) });
      await render();
      await click('查询已绑定账号额度');
      expect(text()).toContain('来源策略状态未知');
      expect(text()).toContain('来源策略待刷新');
      expect(text()).not.toContain('来源策略当前未暂停');
      expect(mocks.quotaQuery).toHaveBeenCalledTimes(1);
      expect(mocks.policyGet).toHaveBeenCalledTimes(1);
    }
  );
  it('批量查询后迟到策略不进入新连接账号', async () => {
    setupQuotaAccount();
    const waiting = deferred<KiloSourcePolicySnapshot>();
    mocks.policyGet.mockReturnValue(waiting.promise);
    await render();
    await click('查询已绑定账号额度');
    expect(text()).toContain('来源策略状态未知');
    const signal = mocks.policyGet.mock.calls[0][2];
    const nextScope = { ...scope, managementKey: 'NEXT_ADMIN' };
    mocks.list.mockResolvedValue({
      connection_ref: 'b'.repeat(32),
      accounts: [
        {
          ...account,
          name: '新连接账号',
          source_policy: { ...readyPolicy, status: 'disabled', enabled: false },
        },
      ],
    });
    await act(async () => renderer.update(<KiloAccountsPanel scope={nextScope} />));
    expect(signal.aborted).toBe(true);
    await act(async () => waiting.resolve({ ...readyPolicy, status: 'paused', paused: true }));
    expect(text()).toContain('新连接账号');
    expect(text()).toContain('来源额度策略已关闭');
    expect(text()).not.toContain('来源额度策略停调');
  });
  it('批量查询后绑定版本变化丢弃旧策略回执', async () => {
    setupQuotaAccount();
    const waiting = deferred<KiloSourcePolicySnapshot>();
    mocks.policyGet.mockReturnValue(waiting.promise);
    await render();
    await click('关联额度');
    const coordinator = renderer.root.findByType(KiloQuotaPanel).props.coordinator;
    await act(async () => {
      void coordinator.runRound();
    });
    await act(async () => coordinator.observe({ ...sourceView, binding_revision: 3 }));
    await act(async () => waiting.resolve({ ...readyPolicy, status: 'paused', paused: true }));
    expect(text()).not.toContain('来源额度策略停调');
    expect(mocks.quotaQuery).toHaveBeenCalledTimes(1);
  });
  it('同一连接可见节拍刷新出口快照，CPA引用变化清旧账号并重读', async () => {
    const state = (ref: string): CredentialConcurrencyState => ({
      snapshot: {
        mode: 'local',
        connection_ref: ref,
        default_limit: 20,
        pool_waiting: 0,
        credentials: [],
      },
      error: '',
      busy: false,
      load: async () => ({ mode: 'local', connection_ref: ref, default_limit: 20, pool_waiting: 0, credentials: [] }),
      refresh: async () => {},
    });
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} concurrency={state(reference)} />);
    });
    expect(mocks.proxyList).toHaveBeenCalledTimes(1);
    await act(async () => {
      renderer.update(<KiloAccountsPanel scope={scope} concurrency={state(reference)} />);
    });
    expect(mocks.proxyList).toHaveBeenCalledTimes(2);
    const nextRef = 'b'.repeat(32);
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, name: '新CPA账号' }],
      connection_ref: nextRef,
    });
    mocks.proxyList.mockResolvedValue({
      revision: 1,
      runtime_revision: 1,
      connection_ref: nextRef,
      accounts: [],
      proxies: [],
      recycle: [],
    });
    await act(async () => {
      renderer.update(<KiloAccountsPanel scope={scope} concurrency={state(nextRef)} />);
    });
    expect(mocks.list).toHaveBeenCalledTimes(2);
    expect(text()).toContain('新CPA账号');
    expect(text()).not.toContain('合成账号');
  });
  it('额度查询同绑定回执同步来源状态，旧版本回执仍被拒绝', async () => {
    const view: KiloQuotaView = {
      account_id: account.id,
      binding_revision: 2,
      connection_ref: reference,
      source: { kind: 'cpa_codex', credential_id: 'codex-source', status: 'ready' },
      query: { status: 'success', attempted_at: null, observed_at: null, error_code: null },
      plan: null,
      windows: [],
      credits: { remaining: null, source_field: null, unit: null },
      stale: false,
    };
    mocks.list.mockResolvedValue({
      accounts: [
        {
          ...account,
          quota_source_summary: {
            kind: 'cpa_codex',
            credential_id: 'codex-source',
            display_label: '关联Codex',
            binding_revision: 2,
            status: 'ready',
          },
          quota_snapshot: view,
        },
      ],
      connection_ref: reference,
    });
    await render();
    await click('关联额度');
    const coordinator = renderer.root.findByType(KiloQuotaPanel).props.coordinator;
    await act(async () => {
      coordinator.observe({ ...view, source: { ...view.source, status: 'needs_sign_in' } });
    });
    expect(
      renderer.root.findByType(KiloQuotaSummary).props.account.quota_source_summary.status
    ).toBe('needs_sign_in');
    expect(() => coordinator.observe({ ...view, binding_revision: 1 })).toThrow();
  });
  it('同scope引用切换后旧写finally不释放新连接写锁', async () => {
    const state = (ref: string): CredentialConcurrencyState => ({
      snapshot: {
        mode: 'local',
        connection_ref: ref,
        default_limit: 20,
        pool_waiting: 0,
        credentials: [],
      },
      error: '',
      busy: false,
      load: async () => ({ mode: 'local', connection_ref: ref, default_limit: 20, pool_waiting: 0, credentials: [] }),
      refresh: async () => {},
    });
    const oldWrite = deferred<Awaited<ReturnType<typeof mocks.update>>>();
    const newWrite = deferred<Awaited<ReturnType<typeof mocks.update>>>();
    mocks.update.mockReturnValueOnce(oldWrite.promise).mockReturnValueOnce(newWrite.promise);
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} concurrency={state(reference)} />);
    });
    await click('停用');
    const nextRef = 'b'.repeat(32);
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, name: '新连接账号' }],
      connection_ref: nextRef,
    });
    await act(async () => {
      renderer.update(<KiloAccountsPanel scope={scope} concurrency={state(nextRef)} />);
    });
    await click('停用');
    await act(async () => {
      oldWrite.resolve({ ...account, connection_ref: reference });
    });
    expect(
      renderer.root.findAllByType(Button).find((button) => button.props.children === '停用')!.props
        .disabled
    ).toBe(true);
    expect(mocks.update).toHaveBeenCalledTimes(2);
    await act(async () => {
      newWrite.resolve({ ...account, connection_ref: nextRef });
    });
    expect(
      renderer.root.findAllByType(Button).find((button) => button.props.children === '停用')!.props
        .disabled
    ).toBe(false);
  });
  it('显示真实计数、默认并发和操作；无未接线字段', async () => {
    await render();
    expect(mocks.list).toHaveBeenCalledWith(scope);
    expect(text()).toContain('合成账号');
    expect(text()).toContain('默认并发20');
    expect(text()).toContain('未设置人工限制');
    expect(text()).toContain('设备授权');
    expect(text()).toContain('组织信息未返回');
    for (const value of ['profile', '代理地址', '回收站']) expect(text()).not.toContain(value);
  });
  it('独立设备入口沿当前引用，打开时阻止旧写操作，保存可刷新而不关闭成功面板', async () => {
    await render();
    await click('设备授权');
    const device = renderer.root.findByType(KiloDeviceAuth);
    expect(device.props.scope).toBe(scope);
    expect(device.props.connectionRef).toBe(reference);
    for (const label of ['导入账号', '刷新列表', '编辑', '停用', '删除']) {
      expect(
        renderer.root.findAllByType(Button).find((b) => b.props.children === label)!.props.disabled
      ).toBe(true);
    }
    await act(async () => device.props.onSaved('saved-id'));
    expect(mocks.list).toHaveBeenCalledTimes(2);
    expect(renderer.root.findAllByType(KiloDeviceAuth)).toHaveLength(1);
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: 'b'.repeat(32) });
    await act(async () => device.props.onSaved('saved-id'));
    expect(renderer.root.findAllByType(KiloDeviceAuth)).toHaveLength(0);
  });
  it('设备入口切换管理连接时关闭，不复用旧会话', async () => {
    await render();
    await click('设备授权');
    await act(async () =>
      renderer.update(<KiloAccountsPanel scope={{ ...scope, apiBase: 'https://next.invalid' }} />)
    );
    expect(renderer.root.findAllByType(KiloDeviceAuth)).toHaveLength(0);
  });
  it('额度入口仅显式打开当前账号与引用，连接变化关闭面板', async () => {
    await render();
    expect(renderer.root.findAllByType(KiloQuotaPanel)).toHaveLength(0);
    await click('关联额度');
    const panel = renderer.root.findByType(KiloQuotaPanel);
    expect(panel.props.accountID).toBe(account.id);
    expect(panel.props.scope).toBe(scope);
    expect(panel.props.connectionRef).toBe(reference);
    expect(
      renderer.root.findAllByType(Button).find((b) => b.props.children === '刷新列表')!.props
        .disabled
    ).toBe(true);
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: 'b'.repeat(32) });
    await act(async () => panel.props.onChanged());
    expect(renderer.root.findAllByType(KiloQuotaPanel)).toHaveLength(0);
  });
  it('加载失败不冒充空列表，刷新后允许操作', async () => {
    mocks.list.mockRejectedValueOnce(new Error('SECRET_SERVER'));
    await render();
    expect(text()).toContain('账号列表读取失败');
    expect(text()).not.toContain('SECRET_SERVER');
    expect(
      renderer.root.findAllByType(Button).find((item) => item.props.children === '导入账号')!.props
        .disabled
    ).toBeTruthy();
    await click('刷新列表');
    expect(text()).toContain('合成账号');
  });
  it('导入提交白名单、空并发为null，提交后Key立即清空且防重复', async () => {
    const pending = deferred<unknown>();
    mocks.create.mockReturnValue(pending.promise);
    await render();
    await click('导入账号');
    await input('账号名称', '新账号');
    await input('API Key', 'SECRET_IMPORT');
    let saving!: Promise<void>;
    await act(async () => {
      saving = renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(
      renderer.root.findAllByType(Input).find((item) => item.props.type === 'password')!.props.value
    ).toBe('');
    await submit();
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.create).toHaveBeenCalledWith(
      {
        name: '新账号',
        api_key: 'SECRET_IMPORT',
        priority: 0,
        enabled: true,
        concurrency_limit: null,
        allow_direct_when_proxy_full: false,
      },
      reference,
      scope
    );
    await act(async () => {
      pending.resolve({ ...account, connection_ref: reference });
      await saving;
    });
    expect(storage.setItem).not.toHaveBeenCalled();
    expect(text()).not.toContain('SECRET_IMPORT');
  });
  it('编辑省略空Key，支持换Key、并发与满载直连', async () => {
    await render();
    await click('编辑');
    await input('账号名称', '已修改');
    await input('优先级', '4');
    await input('并发上限', '7');
    await act(async () => {
      renderer.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label === '健康代理全部满载时允许直连')!
        .props.onChange(true);
    });
    await submit();
    expect(mocks.update.mock.calls[0][1]).toEqual({
      name: '已修改',
      priority: 4,
      concurrency_limit: 7,
      allow_direct_when_proxy_full: true,
    });
    await click('编辑');
    await input('替换API Key（留空保留）', 'SECRET_REPLACEMENT');
    await submit();
    expect(mocks.update.mock.calls[1][1].api_key).toBe('SECRET_REPLACEMENT');
    expect(text()).not.toContain('SECRET_REPLACEMENT');
  });
  it('启停直接提交单字段，删除需要明确确认', async () => {
    await render();
    await click('停用');
    expect(mocks.update).toHaveBeenCalledWith(account.id, { enabled: false }, reference, scope);
    await click('删除');
    expect(mocks.remove).not.toHaveBeenCalled();
    await click('确认删除');
    expect(mocks.remove).toHaveBeenCalledWith(account.id, reference, scope);
  });
  it('取消清空Key；非法并发不提交；失败正文脱敏并提示刷新', async () => {
    await render();
    await click('导入账号');
    await input('API Key', 'SECRET_CANCEL');
    await click('取消');
    await click('导入账号');
    expect(
      renderer.root.findAllByType(Input).find((item) => item.props.type === 'password')!.props.value
    ).toBe('');
    await input('API Key', 'SECRET_INVALID');
    await input('并发上限', '0');
    await submit();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(text()).toContain('并发上限应为');
    await input('API Key', 'SECRET_FAILURE');
    await input('并发上限', '');
    mocks.create.mockRejectedValue({ status: 409, message: 'SECRET_FAILURE' });
    await submit();
    expect(text()).toContain('结果待核');
    expect(text()).not.toContain('SECRET_FAILURE');
    expect(
      renderer.root.findAllByType(Button).find((item) => item.props.children === '导入账号')!.props
        .disabled
    ).toBeTruthy();
  });
  it('切换连接清空Key，迟到写回执不刷新或覆盖新连接', async () => {
    const pending = deferred<unknown>();
    mocks.update.mockReturnValue(pending.promise);
    await render();
    await click('编辑');
    await input('替换API Key（留空保留）', 'SECRET_OLD');
    let saving!: Promise<void>;
    await act(async () => {
      saving = renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    const nextScope = { apiBase: 'https://next.invalid', managementKey: 'NEXT_ADMIN' };
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, name: '新连接账号' }],
      connection_ref: 'b'.repeat(32),
    });
    await act(async () => {
      renderer.update(<KiloAccountsPanel scope={nextScope} />);
    });
    const calls = mocks.list.mock.calls.length;
    await act(async () => {
      pending.resolve({ ...account, connection_ref: reference });
      await saving;
    });
    expect(mocks.list).toHaveBeenCalledTimes(calls);
    expect(text()).toContain('新连接账号');
    expect(text()).not.toContain('SECRET_OLD');
    expect(text()).not.toContain('账号操作已保存');
  });
  it('写后刷新取得新连接引用时释放忙状态且不显示旧成功回执', async () => {
    await render();
    await click('编辑');
    const nextReference = 'b'.repeat(32);
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: nextReference });
    await submit();
    expect(renderer.root.findAllByType('form')).toHaveLength(0);
    expect(text()).not.toContain('账号操作已保存');
    expect(
      renderer.root.findAllByType(Button).find((item) => item.props.children === '停用')!.props
        .disabled
    ).toBe(false);
    await click('停用');
    expect(mocks.update).toHaveBeenCalledTimes(2);
    expect(mocks.update).toHaveBeenLastCalledWith(
      account.id,
      { enabled: false },
      nextReference,
      scope
    );
  });
  it('A连接写成功后手动刷新切到B列表时清除A的成功提示', async () => {
    await render();
    await click('停用');
    expect(text()).toContain('账号操作已保存。');
    expect(mocks.update).toHaveBeenCalledWith(account.id, { enabled: false }, reference, scope);
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, name: 'B连接账号' }],
      connection_ref: 'b'.repeat(32),
    });
    await click('刷新列表');
    expect(mocks.list).toHaveBeenLastCalledWith(scope);
    expect(text()).toContain('B连接账号');
    expect(text()).not.toContain('账号操作已保存');
    expect(mocks.update).toHaveBeenCalledTimes(1);
  });
  it('迟到旧列表与新connection_ref不复用旧表单', async () => {
    const old = deferred<KiloAccountsSnapshot>();
    mocks.list.mockReturnValueOnce(old.promise);
    await render();
    const nextScope = { ...scope, apiBase: 'https://next.invalid' };
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: 'b'.repeat(32) });
    await act(async () => {
      renderer.update(<KiloAccountsPanel scope={nextScope} />);
    });
    await act(async () => {
      old.resolve({ accounts: [{ ...account, name: '迟到账号' }], connection_ref: reference });
    });
    expect(text()).not.toContain('迟到账号');
    await click('编辑');
    await input('替换API Key（留空保留）', 'SECRET_REF');
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: 'c'.repeat(32) });
    await click('刷新列表');
    expect(renderer.root.findAllByType('form')).toHaveLength(0);
    expect(text()).not.toContain('SECRET_REF');
  });
  async function selectOrganization(value: string) {
    await act(async () =>
      renderer.root
        .findAllByType(Select)
        .find((item) => item.props.ariaLabel === '保存组织')!
        .props.onChange(value)
    );
  }
  it('当前组织真实显示，未显式读取不查profile，普通编辑不附带组织字段', async () => {
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, organization_id: 'org_current' }],
      connection_ref: reference,
    });
    await render();
    expect(text()).toContain('org_current');
    await click('编辑');
    expect(text()).toContain('当前组织：');
    expect(mocks.organizations).not.toHaveBeenCalled();
    await input('账号名称', '只改名称');
    await submit();
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('organization_id');
  });
  it('当前Key组织查询不包含新Key；显式选择才提交并以保存DTO确认', async () => {
    await render();
    await click('编辑');
    await input('替换API Key（留空保留）', 'SECRET_NEW_KEY');
    await click('读取当前Key可用组织');
    expect(mocks.organizations).toHaveBeenCalledWith(account.id, scope, expect.any(AbortSignal));
    expect(mocks.organizations.mock.calls[0]).not.toContain('SECRET_NEW_KEY');
    expect(text()).toContain('列表仅属于当前服务端Key');
    expect(text()).toContain('待用Key重新复验');
    await selectOrganization('org_first');
    expect(mocks.update).not.toHaveBeenCalled();
    mocks.update.mockResolvedValue({
      ...account,
      organization_id: 'org_first',
      connection_ref: reference,
    });
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, organization_id: 'org_first' }],
      connection_ref: reference,
    });
    await submit();
    expect(mocks.update.mock.calls[0][1]).toMatchObject({
      api_key: 'SECRET_NEW_KEY',
      organization_id: 'org_first',
    });
    expect(text()).toContain('已保存组织：org_first');
    expect(text()).not.toContain('SECRET_NEW_KEY');
    expect(storage.setItem).not.toHaveBeenCalled();
  });
  it('个人空间只在服务器允许时可选，明确空字符串保存；返回保留不带字段', async () => {
    mocks.organizations.mockResolvedValueOnce({
      organizations: [{ id: 'org_first', name: '第一组织' }],
      has_personal_account: false,
      connection_ref: reference,
    });
    await render();
    await click('编辑');
    await click('读取当前Key可用组织');
    expect(
      renderer.root
        .findByType(Select)
        .props.options.some((item: { value: string }) => item.value === '')
    ).toBe(false);
    expect(text()).toContain('不允许个人空间');
    await selectOrganization('');
    await submit();
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('organization_id');
    await click('编辑');
    await click('读取当前Key可用组织');
    await selectOrganization('');
    await submit();
    expect(mocks.update.mock.calls[1][1]).toHaveProperty('organization_id', '');
    await click('编辑');
    await click('读取当前Key可用组织');
    await selectOrganization('org_first');
    await selectOrganization(':keep');
    await submit();
    expect(mocks.update.mock.calls[2][1]).not.toHaveProperty('organization_id');
  });
  it('组织列表失败保留普通输入及当前账号，不显示底层正文且不冒充保存', async () => {
    mocks.organizations.mockRejectedValueOnce({ status: 503, message: 'SECRET_PROFILE' });
    await render();
    await click('编辑');
    await input('账号名称', '保留输入');
    await input('替换API Key（留空保留）', 'SECRET_DRAFT');
    await click('读取当前Key可用组织');
    expect(text()).toContain('组织列表读取失败');
    expect(text()).not.toContain('SECRET_PROFILE');
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.label === '账号名称')!.props.value
    ).toBe('保留输入');
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.type === 'password')!.props.value
    ).toBe('SECRET_DRAFT');
    expect(mocks.update).not.toHaveBeenCalled();
    expect(text()).not.toContain('账号操作已保存');
  });
  it('换Key保存403保持原组织与非秘密输入，不自动重放；Key提交后清空', async () => {
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, organization_id: 'org_current' }],
      connection_ref: reference,
    });
    mocks.update.mockRejectedValueOnce({ status: 403, message: 'SECRET_DENIED' });
    await render();
    await click('编辑');
    await input('账号名称', '待保存名字');
    await input('替换API Key（留空保留）', 'SECRET_NEW');
    await submit();
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('organization_id');
    expect(text()).toContain('待用Key没有所选组织');
    expect(text()).toContain('org_current');
    expect(text()).not.toContain('SECRET_DENIED');
    expect(mocks.update).toHaveBeenCalledTimes(1);
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.type === 'password')!.props.value
    ).toBe('');
    expect(
      renderer.root.findAllByType(Input).find((i) => i.props.label === '账号名称')!.props.value
    ).toBe('待保存名字');
  });
  it('保存成功但列表刷新失败，显示实际已保存组织而不提前改旧列表', async () => {
    mocks.list.mockResolvedValueOnce({
      accounts: [{ ...account, organization_id: 'org_current' }],
      connection_ref: reference,
    });
    await render();
    await click('编辑');
    await click('读取当前Key可用组织');
    await selectOrganization('org_first');
    mocks.update.mockResolvedValueOnce({
      ...account,
      organization_id: 'org_first',
      connection_ref: reference,
    });
    mocks.list.mockRejectedValueOnce({ status: 503 });
    await submit();
    expect(text()).toContain('账号操作已保存，列表刷新待确认');
    expect(text()).toContain('已保存组织：org_first');
    expect(text()).toContain('org_current');
    expect(mocks.update).toHaveBeenCalledTimes(1);
  });
  it.each(['close', 'scope', 'account', 'reference'] as const)(
    '组织查询%s变化中止请求且丢弃迟到组织列表',
    async (change) => {
      const pending = deferred<KiloOrganizations>();
      mocks.organizations.mockReturnValueOnce(pending.promise);
      await render();
      await click('编辑');
      await click('读取当前Key可用组织');
      const signal = mocks.organizations.mock.calls[0][2] as AbortSignal;
      if (change === 'scope') {
        await act(async () =>
          renderer.update(
            <KiloAccountsPanel scope={{ ...scope, apiBase: 'https://next.invalid' }} />
          )
        );
      } else if (change === 'reference') {
        mocks.list.mockResolvedValueOnce({ accounts: [account], connection_ref: 'b'.repeat(32) });
        await click('刷新列表');
      } else {
        await click('取消');
        if (change === 'account') {
          mocks.list.mockResolvedValueOnce({
            accounts: [{ ...account, id: 'next-account', name: '下一账号' }],
            connection_ref: reference,
          });
          await click('刷新列表');
          await click('编辑');
        }
      }
      expect(signal.aborted).toBe(true);
      await act(async () =>
        pending.resolve({
          organizations: [{ id: 'org_late', name: '迟到组织' }],
          has_personal_account: true,
          connection_ref: reference,
        })
      );
      expect(text()).not.toContain('迟到组织');
      expect(mocks.update).not.toHaveBeenCalled();
    }
  );
  it('组织查询回执引用不符和本地中止均不接受列表，重复读取单在途', async () => {
    mocks.organizations.mockResolvedValueOnce({
      organizations: [{ id: 'org_late', name: '错误连接组织' }],
      has_personal_account: true,
      connection_ref: 'b'.repeat(32),
    });
    await render();
    await click('编辑');
    await click('读取当前Key可用组织');
    expect(text()).toContain('连接已变化');
    expect(text()).not.toContain('错误连接组织');
    const pending = deferred<KiloOrganizations>();
    mocks.organizations.mockReturnValueOnce(pending.promise);
    const readButton = renderer.root
      .findAllByType(Button)
      .find((b) => b.props.children === '读取当前Key可用组织')!;
    await act(async () => {
      readButton.props.onClick();
      readButton.props.onClick();
    });
    expect(mocks.organizations).toHaveBeenCalledTimes(2);
    const signal = mocks.organizations.mock.calls[1][2] as AbortSignal;
    await click('中止组织查询');
    expect(signal.aborted).toBe(true);
    await act(async () =>
      pending.resolve({
        organizations: [{ id: 'org_late', name: '迟到组织' }],
        has_personal_account: true,
        connection_ref: reference,
      })
    );
    expect(text()).toContain('组织查询已中止');
    expect(text()).not.toContain('迟到组织');
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
