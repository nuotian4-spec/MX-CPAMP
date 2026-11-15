import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import type { PropsWithChildren } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { KiloAccount } from '@/services/api/kilo';
import type { KiloProbesSnapshot, KiloProbesConfig } from '@/services/api/kiloProbes';
import { KiloProbesPanel } from './KiloProbesPanel';
import { KiloAccountsPanel } from './KiloAccountsPage';

declare const process: { env: Record<string, string | undefined> };
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  save: vi.fn(),
  models: vi.fn(),
  list: vi.fn(),
  update: vi.fn(),
  onClose: vi.fn(),
}));
vi.mock('@/services/api/kiloProbes', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProbes')>()),
  kiloProbesApi: { get: mocks.get, save: mocks.save },
}));
vi.mock('@/services/api/kiloManual', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloManual')>()),
  kiloManualApi: { models: mocks.models },
}));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: { list: mocks.list, update: mocks.update },
}));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('react-router-dom', () => ({
  Link: ({ children }: PropsWithChildren) => <span>{children}</span>,
}));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: PropsWithChildren) => <section>{children}</section>,
}));
vi.mock('./KiloDeviceAuth', () => ({ KiloDeviceAuth: () => null }));
vi.mock('./KiloQuotaPanel', () => ({ KiloQuotaPanel: () => null }));
vi.mock('./KiloQuotaBatch', () => ({ KiloQuotaBatch: () => null }));
const scope = { apiBase: 'http://manager.invalid', managementKey: 'SYNTHETIC' };
const ref = 'a'.repeat(32);
const account: KiloAccount = {
  id: 'probe.json',
  name: '合成账号',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
};
const snapshot = (): KiloProbesSnapshot => ({
  account_id: account.id,
  connection_ref: ref,
  revision: 0,
  runtime_started_at: '2026-11-13T00:00:00Z',
  manual_model_calls: 2,
  health: {
    enabled: false,
    model: 'kilo-auto/free',
    interval_seconds: 300,
    status: 'never',
    next_run_at: null,
    last_started_at: null,
    last_completed_at: null,
    error_code: null,
    model_calls: 0,
    skipped_count: 0,
  },
  model_probe: {
    enabled: false,
    model: '',
    interval_seconds: 300,
    status: 'never',
    next_run_at: null,
    last_started_at: null,
    last_completed_at: null,
    error_code: null,
    model_calls: 0,
    skipped_count: 0,
  },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
let renderer: ReactTestRenderer | undefined;
const text = () => JSON.stringify(renderer!.toJSON());
async function mount() {
  await act(async () => {
    renderer = create(
      <KiloProbesPanel
        account={account}
        scope={scope}
        connectionRef={ref}
        onClose={mocks.onClose}
      />
    );
  });
}
async function click(label: string) {
  const button = renderer!.root.findAllByType(Button).find((node) => node.props.children === label);
  if (!button) throw Error('按钮不存在：' + label);
  await act(async () => {
    button.props.onClick();
  });
}
async function toggle(label: string, value: boolean) {
  await act(async () => {
    renderer!.root
      .findAllByType(ToggleSwitch)
      .find((node) => node.props.label === label)!
      .props.onChange(value);
  });
}
async function select(model: string, interval = '300') {
  await act(async () => {
    renderer!.root.findByProps({ ariaLabel: '探测模型' }).props.onChange(model);
    renderer!.root
      .findByProps({ 'aria-label': '探测周期秒数' })
      .props.onChange({ target: { value: interval } });
  });
}
async function save() {
  await act(async () => {
    void renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
  });
}
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', Object.assign(new EventTarget(), { location: { hash: '', search: '' } }));
  vi.stubGlobal('localStorage', { setItem: vi.fn() });
  vi.stubGlobal('sessionStorage', { setItem: vi.fn() });
  mocks.get.mockResolvedValue(snapshot());
  mocks.models.mockResolvedValue({ models: [{ id: 'gpt-6.1-sol' }], connection_ref: ref });
  mocks.list.mockResolvedValue({ accounts: [account], connection_ref: ref });
  mocks.save.mockImplementation(async (_id: string, input: KiloProbesConfig) => ({
    ...snapshot(),
    revision: input.expected_revision + 1,
    health: {
      ...snapshot().health,
      ...input.health,
      status: input.health.enabled ? 'scheduled' : 'never',
    },
    model_probe: {
      ...snapshot().model_probe,
      ...input.model_probe,
      status: input.model_probe.enabled ? 'scheduled' : 'never',
    },
  }));
});
afterEach(async () => {
  if (renderer) await act(async () => renderer!.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});
describe('自动生成管理组件', () => {
  it('真实账号行默认不读取探测/生成；显式打开只读默认双关，不保存或暗启用', async () => {
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} />);
    });
    expect(mocks.get).not.toHaveBeenCalled();
    expect(mocks.save).not.toHaveBeenCalled();
    await click('自动生成配置');
    expect(mocks.get).toHaveBeenCalledTimes(1);
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.update).not.toHaveBeenCalled();
    expect(
      renderer!.root
        .findAllByType(ToggleSwitch)
        .filter((node) => node.props.label.startsWith('开启'))
        .map((node) => node.props.checked)
    ).toEqual([false, false]);
    expect(text()).toContain('可能计费');
    expect(text()).toContain('当前宿主计数起点');
    expect(text()).toContain('不是当前模型专属次数');
    expect(localStorage.setItem).not.toHaveBeenCalled();
    expect(sessionStorage.setItem).not.toHaveBeenCalled();
  });
  it('编辑一类保留另一类已读配置，完整双配置和revision/ref只保存一次', async () => {
    mocks.get.mockResolvedValue({
      ...snapshot(),
      revision: 8,
      model_probe: {
        ...snapshot().model_probe,
        enabled: true,
        model: 'gpt-6.1-sol',
        interval_seconds: 600,
        status: 'scheduled',
      },
    });
    await mount();
    await toggle('开启账号健康生成', true);
    await save();
    expect(mocks.save).toHaveBeenCalledWith(
      account.id,
      {
        expected_revision: 8,
        health: { enabled: true },
        model_probe: { enabled: true, model: 'gpt-6.1-sol', interval_seconds: 600 },
      },
      ref,
      scope,
      expect.any(AbortSignal)
    );
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(text()).toContain('已计划');
    expect(text()).not.toContain('生成完成');
  });
  it.each(['59', '86401', '60.5'])('非法周期%s和空模型不保存', async (period) => {
    await mount();
    await toggle('开启模型周期探测', true);
    await save();
    expect(mocks.save).not.toHaveBeenCalled();
    await select('gpt-6.1-sol', period);
    await save();
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it.each([400, 409, 503])('HTTP%s失败保旧状态，阻止重复保存直到重新读取', async (status) => {
    mocks.save.mockRejectedValue({ status, message: 'SECRET' });
    await mount();
    await toggle('开启账号健康生成', true);
    await save();
    await save();
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(text()).toContain('已保存开关：');
    expect(text()).not.toContain('配置已保存');
    expect(text()).not.toContain('SECRET');
    expect(
      renderer!.root
        .findAllByType(Button)
        .find((node) => node.props.children === '保存自动生成配置')!.props.disabled
    ).toBe(true);
    await click('重新读取状态');
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });
  it('运行中重复保存只有一个PUT，服务器回包前不发布成功', async () => {
    const pending = deferred<KiloProbesSnapshot>();
    mocks.save.mockReturnValue(pending.promise);
    await mount();
    await toggle('开启账号健康生成', true);
    await save();
    await save();
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(text()).not.toContain('配置已保存');
    await act(async () => pending.resolve({ ...snapshot(), revision: 1 }));
    expect(text()).toContain('配置已保存');
  });
  it.each([
    'scheduled',
    'running',
    'completed',
    'incomplete',
    'failed',
    'canceled',
    'skipped',
  ] as const)('按真实%s投影展示状态/时间/类别计数', async (status) => {
    const view = snapshot();
    view.health = {
      ...view.health,
      status,
      model_calls: 5,
      skipped_count: 3,
      error_code: status === 'skipped' ? 'account_busy' : null,
      last_started_at: '2026-11-13T01:00:00Z',
    };
    mocks.get.mockResolvedValue(view);
    await mount();
    expect(text()).toContain('2026-11-13T01:00:00Z');
    expect(text()).toContain('模型发送尝试：');
    if (status === 'skipped') expect(text()).toContain('同账号自动任务忙碌');
    if (status !== 'completed') expect(text()).not.toContain('生成完成');
  });
  it('切换模型后只展示服务端新模型投影，保类别累计不挂旧终态', async () => {
    const view = snapshot();
    view.model_probe = {
      ...view.model_probe,
      enabled: true,
      model: 'gpt-6.1-sol',
      status: 'running',
      model_calls: 4,
      last_started_at: 'OLD_MODEL_TIME',
    };
    mocks.get.mockResolvedValue(view);
    mocks.save.mockResolvedValue({
      ...view,
      revision: 1,
      model_probe: {
        ...view.model_probe,
        model: 'kilo-auto/free',
        status: 'scheduled',
        last_started_at: null,
        last_completed_at: null,
        error_code: null,
      },
    });
    await mount();
    await select('kilo-auto/free');
    await save();
    expect(text()).not.toContain('OLD_MODEL_TIME');
    expect(text()).toContain('已计划');
    expect(text()).toContain('不是当前模型专属次数');
  });
  it.each(['关闭', '切账号', '切连接', '失认证'])('%s中止保存并丢弃迟到回包', async (action) => {
    const pending = deferred<KiloProbesSnapshot>();
    mocks.save.mockReturnValue(pending.promise);
    await mount();
    await save();
    const signal = mocks.save.mock.calls[0][4] as AbortSignal;
    await act(async () => {
      if (action === '关闭') {
        renderer!.unmount();
        renderer = undefined;
      } else if (action === '失认证') window.dispatchEvent(new Event('unauthorized'));
      else
        renderer!.update(
          <KiloProbesPanel
            account={action === '切账号' ? { ...account, id: 'other.json' } : account}
            scope={action === '切连接' ? { ...scope, apiBase: 'http://other.invalid' } : scope}
            connectionRef={ref}
            onClose={mocks.onClose}
          />
        );
    });
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve({ ...snapshot(), revision: 99 }));
    if (renderer) expect(text()).not.toContain('配置已保存');
  });
  it('错误账号或引用回包不发布配置', async () => {
    mocks.save.mockResolvedValue({ ...snapshot(), connection_ref: 'b'.repeat(32) });
    await mount();
    await save();
    expect(text()).not.toContain('配置已保存');
    expect(text()).toContain('原目标状态待核');
  });
  it.skipIf(!process.env.KILO_PROBES_MANAGER_URL || !process.env.KILO_PROBES_CONTROL_URL)(
    '真实账号页经Manager与Core保存双类自动配置并读取两轮结果',
    async () => {
      const managerURL = process.env.KILO_PROBES_MANAGER_URL!,
        controlURL = process.env.KILO_PROBES_CONTROL_URL!;
      for (const raw of [managerURL, controlURL]) {
        const u = new URL(raw);
        expect(u.protocol).toBe('http:');
        expect(['127.0.0.1', '[::1]']).toContain(u.hostname);
      }
      const fixture = (await (await fetch(controlURL + '/fixture')).json()) as {
        account_id: string;
        normal_model: string;
      };
      const probes = await vi.importActual<typeof import('@/services/api/kiloProbes')>(
        '@/services/api/kiloProbes'
      );
      const manual = await vi.importActual<typeof import('@/services/api/kiloManual')>(
        '@/services/api/kiloManual'
      );
      const accounts =
        await vi.importActual<typeof import('@/services/api/kilo')>('@/services/api/kilo');
      const jointScope = {
        apiBase: managerURL,
        managementKey: process.env.KILO_PROBES_MANAGER_KEY!,
      };
      const { apiClient } = await import('@/services/api/client');
      apiClient.setConfig(jointScope);
      mocks.get.mockImplementation(probes.kiloProbesApi.get);
      mocks.save.mockImplementation(probes.kiloProbesApi.save);
      mocks.models.mockImplementation(manual.kiloManualApi.models);
      mocks.list.mockImplementation(accounts.kiloApi.list);
      const until = async (predicate: () => boolean) => {
        const deadline = Date.now() + 15000;
        while (!predicate()) {
          const alerts = renderer!.root.findAllByProps({ role: 'alert' });
          if (alerts.length) throw Error(alerts.map((n) => n.children.join(' ')).join(';'));
          if (Date.now() > deadline) throw Error('自动生成组件等待超时');
          await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 20));
          });
        }
      };
      const control = async (path: string, body: unknown) => {
        const res = await fetch(controlURL + path, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        expect(res.status).toBe(200);
      };
      await act(async () => {
        renderer = create(<KiloAccountsPanel scope={jointScope} />);
      });
      await until(() =>
        renderer!.root
          .findAllByType(Button)
          .some((button) => button.props.children === '自动生成配置')
      );
      const menu = renderer!.root
        .findAllByType(Button)
        .find((button) => button.props.children === '自动生成配置')!;
      await act(async () => {
        menu.props.onClick();
      });
      await until(
        () => renderer!.root.findAllByProps({ 'aria-label': '自动生成已读取状态' }).length === 1
      );
      expect(mocks.save).not.toHaveBeenCalled();
      await control('/advance', { phase: 'defaults' });
      await toggle('开启账号健康生成', true);
      await toggle('开启模型周期探测', true);
      await select(fixture.normal_model);
      await save();
      await until(() => text().includes('配置已保存'));
      await control('/advance', { phase: 'before_due' });
      await control('/advance', { phase: 'first_due' });
      await click('重新读取状态');
      await until(
        () =>
          !renderer!.root.findAllByType(Button).find((b) => b.props.children === '重新读取状态')!
            .props.disabled
      );
      await control('/advance', { phase: 'second_due' });
      await click('重新读取状态');
      await until(
        () =>
          !renderer!.root.findAllByType(Button).find((b) => b.props.children === '重新读取状态')!
            .props.disabled
      );
      const latest = (await mocks.get.mock.results[mocks.get.mock.results.length - 1]
        .value) as KiloProbesSnapshot;
      expect(latest.health.model_calls).toBe(1);
      expect(latest.model_probe.model_calls).toBe(1);
      await toggle('开启账号健康生成', false);
      await toggle('开启模型周期探测', false);
      await save();
      await until(() => text().includes('配置已保存'));
      expect(mocks.save).toHaveBeenCalledTimes(2);
      expect(mocks.update).not.toHaveBeenCalled();
      await control('/verify', {
        account_id: fixture.account_id,
        normal_model: fixture.normal_model,
      });
    },
    60000
  );
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
