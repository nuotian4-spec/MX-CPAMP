import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloAccountsPanel } from './KiloAccountsPage';
const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), query: vi.fn() }));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: { list: mocks.list },
}));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: { get: mocks.get, query: mocks.query },
}));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'KEY' },
  reference = 'a'.repeat(32);
const account = {
  id: 'a',
  name: 'a',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
};
const view = {
  account_id: 'a',
  binding_revision: 1,
  connection_ref: reference,
  source: { kind: 'imported', status: 'ready' },
  query: { status: 'never', attempted_at: null, observed_at: null, error_code: null },
  windows: [],
  plan: null,
  credits: { remaining: null, unit: null, source_field: null },
  stale: false,
};
const flush = async () => {
  for (let i = 0; i < 15; i++) await Promise.resolve();
};
describe('额度任务依附真实账号页生命周期', () => {
  let renderer: ReactTestRenderer | undefined;
  let finish: ((value: unknown) => void) | undefined;
  beforeEach(() => {
    vi.resetAllMocks();
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: reference });
    mocks.get.mockResolvedValue(view);
    mocks.query.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        })
    );
  });
  afterEach(async () => {
    await act(async () => {
      renderer?.unmount();
      finish?.({
        view: { ...view, query: { ...view.query, status: 'success' } },
        cancelled: false,
      });
      await flush();
    });
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(
        <MemoryRouter>
          <KiloAccountsPanel scope={scope} />
        </MemoryRouter>
      );
      await flush();
    });
    await act(async () => {
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label === '本页定时查询（30分钟）')!
        .props.onChange(true);
      await flush();
    });
  }
  it('卸载停止页面在途和后续30分钟轮次', async () => {
    await render();
    expect(mocks.query).toHaveBeenCalledTimes(1);
    await act(async () => {
      renderer!.unmount();
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query.mock.calls[0][4].aborted).toBe(true);
    expect(mocks.query).toHaveBeenCalledTimes(1);
    renderer = undefined;
  });
  it('切连接清旧定时和成功提示，新连接默认关闭', async () => {
    await render();
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: 'b'.repeat(32) });
    await act(async () => {
      renderer!.update(
        <MemoryRouter>
          <KiloAccountsPanel scope={{ ...scope, apiBase: 'https://second.invalid' }} />
        </MemoryRouter>
      );
      await flush();
    });
    await act(async () => {
      finish?.({
        view: { ...view, query: { ...view.query, status: 'success' } },
        cancelled: false,
      });
      await flush();
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query.mock.calls[0][4].aborted).toBe(true);
    expect(mocks.query).toHaveBeenCalledTimes(1);
    expect(
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label === '本页定时查询（30分钟）')!.props.checked
    ).toBe(false);
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('查询成功');
  });
  it('管理失认证事件立即停止本页在途', async () => {
    const target = new EventTarget();
    vi.stubGlobal('window', target);
    await render();
    await act(async () => {
      target.dispatchEvent(new Event('unauthorized'));
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query.mock.calls[0][4].aborted).toBe(true);
    expect(mocks.query).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(renderer!.toJSON())).toContain('管理认证已失效');
  });
});
