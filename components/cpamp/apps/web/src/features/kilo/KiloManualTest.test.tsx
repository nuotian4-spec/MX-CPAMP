import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import type { PropsWithChildren } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { KiloManualTest } from './KiloManualTest';
import { KiloAccountsPanel } from './KiloAccountsPage';
import type { KiloAccount } from '@/services/api/kilo';
import type { KiloManualResult } from '@/services/api/kiloManual';

declare const process: { env: Record<string, string | undefined> };
const mocks = vi.hoisted(() => ({
  models: vi.fn(),
  test: vi.fn(),
  list: vi.fn(),
  onClose: vi.fn(),
  update: vi.fn(),
}));
vi.mock('@/services/api/kiloManual', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloManual')>()),
  kiloManualApi: { models: mocks.models, test: mocks.test },
}));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: { list: mocks.list, update: mocks.update },
}));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('react-router-dom', () => ({
  Link: ({ children, to }: PropsWithChildren<{ to: string }>) => <a href={to}>{children}</a>,
}));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: PropsWithChildren) => <div role="dialog">{children}</div>,
}));
vi.mock('./KiloDeviceAuth', () => ({ KiloDeviceAuth: () => null }));
vi.mock('./KiloQuotaPanel', () => ({ KiloQuotaPanel: () => null }));
vi.mock('./KiloQuotaBatch', () => ({ KiloQuotaBatch: () => null }));
const scope = { apiBase: 'http://manager.invalid', managementKey: 'SYNTHETIC_ADMIN' };
const ref = 'a'.repeat(32);
const account: KiloAccount = {
  id: 'manual.json',
  name: '合成停用账号',
  enabled: false,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
};
const result: KiloManualResult = {
  account_id: account.id,
  connection_ref: ref,
  model: 'gpt-6.1-sol',
  status: 'completed',
  started_at: '2026-11-13T00:00:00Z',
  completed_at: '2026-11-13T00:00:01Z',
  duration_ms: 1000,
  text: '<b>纯文本</b>',
  error_code: null,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
let renderer: ReactTestRenderer | undefined;
const text = () => JSON.stringify(renderer!.toJSON());
async function click(label: string) {
  const button = renderer!.root.findAllByType(Button).find((node) => node.props.children === label);
  if (!button) throw Error('按钮不存在:' + label);
  await act(async () => {
    button.props.onClick();
  });
}
async function fill(model = 'gpt-6.1-sol', prompt = '  明确提示词  ') {
  await act(async () => {
    renderer!.root.findByProps({ ariaLabel: '测试模型' }).props.onChange(model);
    renderer!.root
      .findByProps({ 'aria-label': '文本提示词' })
      .props.onChange({ target: { value: prompt } });
  });
}
async function submit() {
  await act(async () => {
    void renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
  });
}
async function mount() {
  await act(async () => {
    renderer = create(
      <KiloManualTest account={account} scope={scope} connectionRef={ref} onClose={mocks.onClose} />
    );
  });
}
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', Object.assign(new EventTarget(), { location: { hash: '', search: '' } }));
  vi.stubGlobal('localStorage', { setItem: vi.fn() });
  vi.stubGlobal('sessionStorage', { setItem: vi.fn() });
  mocks.models.mockResolvedValue({ models: [{ id: 'gpt-6.1-sol' }], connection_ref: ref });
  mocks.test.mockResolvedValue(result);
  mocks.list.mockResolvedValue({ accounts: [account], connection_ref: ref });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer!.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});
describe('单账号手动模型测试', () => {
  it('真实账号页默认不生成，停用账号显式打开后才读取目录且不暗启用', async () => {
    await act(async () => {
      renderer = create(<KiloAccountsPanel scope={scope} />);
    });
    expect(mocks.models).not.toHaveBeenCalled();
    expect(mocks.test).not.toHaveBeenCalled();
    await click('手动模型测试');
    expect(mocks.models).toHaveBeenCalledTimes(1);
    expect(mocks.test).not.toHaveBeenCalled();
    expect(text()).toContain('人工停用');
    expect(text()).toContain(scope.apiBase);
    await fill();
    await submit();
    expect(mocks.test).toHaveBeenCalledTimes(1);
    expect(mocks.test).toHaveBeenCalledWith(
      account.id,
      'gpt-6.1-sol',
      '  明确提示词  ',
      ref,
      scope,
      expect.any(AbortSignal)
    );
    expect(mocks.update).not.toHaveBeenCalled();
    expect(account.enabled).toBe(false);
    expect(text()).toContain('测试完成');
    expect(localStorage.setItem).not.toHaveBeenCalled();
    expect(sessionStorage.setItem).not.toHaveBeenCalled();
  });
  it.each(['completed', 'incomplete', 'failed', 'canceled'] as const)(
    '按%s展示结果，不按HTTP200判断成功',
    async (status) => {
      mocks.test.mockResolvedValue({ ...result, status, error_code: 'SECRET_ERROR' });
      await mount();
      await fill();
      await submit();
      const labels = {
        completed: '测试完成',
        incomplete: '回答未完成',
        failed: '测试失败',
        canceled: '测试已取消',
      };
      expect(text()).toContain(labels[status]);
      expect(text()).not.toContain('SECRET_ERROR');
      const pre = renderer!.root.findAllByType('pre');
      if (status === 'completed' || status === 'incomplete')
        expect(pre[0].children).toEqual(['<b>纯文本</b>']);
      else expect(pre).toHaveLength(0);
      expect(renderer!.root.findAllByType('b')).toHaveLength(0);
    }
  );
  it('空输入/未知模型不提交；运行中重复提交不增请求', async () => {
    const pending = deferred<KiloManualResult>();
    mocks.test.mockReturnValue(pending.promise);
    await mount();
    await submit();
    expect(mocks.test).not.toHaveBeenCalled();
    await fill('other');
    await submit();
    expect(mocks.test).not.toHaveBeenCalled();
    await fill('kilo-auto/free');
    await submit();
    await submit();
    expect(mocks.test).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve({ ...result, model: 'kilo-auto/free' }));
  });
  it('取消请求会中止信号，丢弃迟到文本且不宣称服务端完成', async () => {
    const pending = deferred<KiloManualResult>();
    mocks.test.mockReturnValue(pending.promise);
    await mount();
    await fill();
    await submit();
    const signal = mocks.test.mock.calls[0][5] as AbortSignal;
    await click('取消本次请求');
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve(result));
    expect(text()).not.toContain('纯文本');
    expect(text()).toContain('服务端最终结果未确认');
  });
  it.each(['关闭', '切账号', '切连接', '失认证'])('%s中止操作并丢弃旧结果', async (action) => {
    const pending = deferred<KiloManualResult>();
    mocks.test.mockReturnValue(pending.promise);
    await mount();
    await fill();
    await submit();
    const signal = mocks.test.mock.calls[0][5] as AbortSignal;
    await act(async () => {
      if (action === '关闭') {
        renderer!.unmount();
        renderer = undefined;
      } else if (action === '失认证') window.dispatchEvent(new Event('unauthorized'));
      else
        renderer!.update(
          <KiloManualTest
            account={action === '切账号' ? { ...account, id: 'new.json' } : account}
            scope={action === '切连接' ? { ...scope, apiBase: 'http://new.invalid' } : scope}
            connectionRef={ref}
            onClose={mocks.onClose}
          />
        );
    });
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve(result));
    if (renderer) expect(text()).not.toContain('纯文本');
  });
  it.each([400, 401, 404, 409, 429, 503])(
    'HTTP%d分类固定提示，不展示上游错误正文',
    async (status) => {
      mocks.test.mockRejectedValue({ status, message: 'SECRET_UPSTREAM' });
      await mount();
      await fill();
      await submit();
      expect(text()).not.toContain('SECRET_UPSTREAM');
      expect(renderer!.root.findAllByProps({ role: 'alert' })).toHaveLength(1);
      expect(mocks.test).toHaveBeenCalledTimes(1);
    }
  );
  it('回执账号或连接引用不符不发布文本', async () => {
    mocks.test.mockResolvedValue({ ...result, connection_ref: 'b'.repeat(32) });
    await mount();
    await fill();
    await submit();
    expect(text()).not.toContain('纯文本');
    expect(text()).toContain('原测试结果待核');
  });
  it.skipIf(!process.env.KILO_MANUAL_MANAGER_URL || !process.env.KILO_MANUAL_CONTROL_URL)(
    '真实账号页经Manager与Core执行Normal及Auto Free单账号测试',
    async () => {
      const managerURL = process.env.KILO_MANUAL_MANAGER_URL!;
      const controlURL = process.env.KILO_MANUAL_CONTROL_URL!;
      for (const raw of [managerURL, controlURL]) {
        const u = new URL(raw);
        expect(u.protocol).toBe('http:');
        expect(['127.0.0.1', '[::1]']).toContain(u.hostname);
      }
      const fixture = (await (await fetch(controlURL + '/fixture')).json()) as {
        account_id: string;
        normal_model: string;
      };
      const actual = await vi.importActual<typeof import('@/services/api/kiloManual')>(
        '@/services/api/kiloManual'
      );
      const accounts =
        await vi.importActual<typeof import('@/services/api/kilo')>('@/services/api/kilo');
      const jointScope = {
        apiBase: managerURL,
        managementKey: process.env.KILO_MANUAL_MANAGER_KEY!,
      };
      const { apiClient } = await import('@/services/api/client');
      apiClient.setConfig(jointScope);
      mocks.models.mockImplementation(actual.kiloManualApi.models);
      mocks.test.mockImplementation(actual.kiloManualApi.test);
      mocks.list.mockImplementation(accounts.kiloApi.list);
      const until = async (predicate: () => boolean) => {
        const deadline = Date.now() + 15000;
        while (!predicate()) {
          const errors = renderer!.root.findAllByProps({ role: 'alert' });
          if (errors.length) throw Error(errors.map((n) => n.children.join(' ')).join('; '));
          if (Date.now() > deadline) throw Error('手动测试组件等待超时');
          await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 20));
          });
        }
      };
      await act(async () => {
        renderer = create(<KiloAccountsPanel scope={jointScope} />);
      });
      await until(() =>
        renderer!.root
          .findAllByType(Button)
          .some((button) => button.props.children === '手动模型测试')
      );
      const menu = renderer!.root
        .findAllByType(Button)
        .find((button) => button.props.children === '手动模型测试')!;
      await act(async () => {
        menu.props.onClick();
      });
      await until(() =>
        renderer!.root
          .findAllByProps({ ariaLabel: '测试模型' })
          .some((node) =>
            node.props.options?.some(
              (option: { value: string }) => option.value === fixture.normal_model
            )
          )
      );
      for (const model of [fixture.normal_model, 'kilo-auto/free']) {
        await fill(
          model,
          model === 'kilo-auto/free' ? 'M8M auto synthetic' : 'M8M normal synthetic'
        );
        await submit();
        await until(
          () => renderer!.root.findAllByProps({ 'aria-label': '本次手动测试结果' }).length === 1
        );
        expect(text()).toContain('测试完成');
        expect(renderer!.root.findByType('pre').children.join('')).not.toBe('');
      }
      expect(mocks.test).toHaveBeenCalledTimes(2);
      expect(mocks.update).not.toHaveBeenCalled();
      const verified = await fetch(controlURL + '/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          account_id: fixture.account_id,
          normal_model: fixture.normal_model,
          tests: 2,
        }),
      });
      expect(verified.status).toBe(200);
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
