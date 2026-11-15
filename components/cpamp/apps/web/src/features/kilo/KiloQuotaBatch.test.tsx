import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloQuotaBatch } from './KiloQuotaBatch';
import { KiloQuotaCoordinator } from './KiloQuotaCoordinator';
import { KiloQuotaPanel } from './KiloQuotaPanel';
import type { KiloQuotaView } from '@/services/api/kiloQuota';
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  getPolicy: vi.fn(),
  putPolicy: vi.fn(),
  query: vi.fn(),
  sources: vi.fn(),
  put: vi.fn(),
  remove: vi.fn(),
}));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: mocks,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'FIXTURE' },
  ref = 'a'.repeat(32);
function view(id: string, revision = 1): KiloQuotaView {
  return {
    account_id: id,
    connection_ref: ref,
    binding_revision: revision,
    source: { kind: 'cpa_codex', credential_id: 'same-codex', status: 'ready' },
    query: { status: 'never', attempted_at: null, observed_at: null, error_code: null },
    windows: [],
    plan: null,
    stale: false,
    credits: { remaining: null, unit: null, source_field: null },
  };
}
function result(id: string) {
  return {
    view: {
      ...view(id),
      query: {
        status: 'success' as const,
        attempted_at: '2026-11-13T00:00:00Z',
        observed_at: '2026-11-13T00:00:00Z',
        error_code: null,
      },
    },
    cancelled: false,
  };
}
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
const flush = async () => {
  for (let i = 0; i < 12; i++) await Promise.resolve();
};
describe('Kilo页面批量和定时协调', () => {
  let renderer: ReactTestRenderer | undefined, coordinator: KiloQuotaCoordinator;
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.getPolicy.mockResolvedValue({
      enabled: false,
      interval_seconds: 1800,
      revision: 0,
      paused: false,
      pause_generation: 0,
      scope: '',
      status: 'disabled',
      observed_at: null,
      next_query_at: null,
      error_code: null,
      persisted: true,
      connection_ref: ref,
    });
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    coordinator = new KiloQuotaCoordinator(scope, ref);
    coordinator.setAccounts(['a', 'b', 'c', 'd']);
    mocks.get.mockImplementation(async (id: string) => view(id));
    mocks.query.mockImplementation(async (id: string) => result(id));
  });
  afterEach(async () => {
    await act(async () => {
      renderer?.unmount();
      coordinator.dispose();
      await flush();
    });
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  const render = async (manual = false) => {
    await act(async () => {
      renderer = create(
        <>
          <KiloQuotaBatch coordinator={coordinator} />
          {manual && (
            <KiloQuotaPanel
              accountID="a"
              scope={scope}
              connectionRef={ref}
              coordinator={coordinator}
              onClose={() => {}}
              onChanged={async () => {}}
            />
          )}
        </>
      );
    });
  };
  const click = async (label: string) => {
    await act(async () => {
      renderer!.root
        .findAllByType(Button)
        .find((b) => b.props.children === label)!
        .props.onClick();
      await flush();
    });
  };
  const text = () => JSON.stringify(renderer!.toJSON());
  it('四账号同Codex独立查询，最多三在途，完成一项补一项', async () => {
    const pending = new Map<string, ReturnType<typeof deferred<ReturnType<typeof result>>>>();
    let active = 0,
      max = 0;
    mocks.query.mockImplementation((id: string) => {
      const d = deferred<ReturnType<typeof result>>();
      pending.set(id, d);
      active++;
      max = Math.max(active, max);
      return d.promise.finally(() => active--);
    });
    await render();
    await click('查询已绑定账号额度');
    expect(mocks.query).toHaveBeenCalledTimes(3);
    expect(max).toBe(3);
    expect(pending.has('d')).toBe(false);
    await act(async () => {
      pending.get('b')!.resolve(result('b'));
      await flush();
    });
    expect(mocks.query).toHaveBeenCalledTimes(4);
    expect(max).toBe(3);
    await act(async () => {
      for (const [id, d] of pending) d.resolve(result(id));
      await flush();
    });
    expect(text()).toContain('成功 ');
    expect(coordinator.snapshot().running).toBe(false);
    expect(
      Object.values(coordinator.snapshot().outcomes).every((v) => v.status === 'success')
    ).toBe(true);
    expect(Object.keys(coordinator.snapshot().views)).toEqual(['a', 'b', 'c', 'd']);
  });
  it('真实单条面板与批量重合只发一次POST并复用同一结果', async () => {
    const d = deferred<ReturnType<typeof result>>();
    mocks.query.mockImplementation((id: string) =>
      id === 'a' ? d.promise : Promise.resolve(result(id))
    );
    await render(true);
    await click('查询一次额度');
    await click('查询已绑定账号额度');
    expect(mocks.query.mock.calls.filter((c) => c[0] === 'a')).toHaveLength(1);
    await act(async () => {
      d.resolve(result('a'));
      await flush();
    });
    expect(text()).toContain('本次额度查询成功');
    expect(coordinator.snapshot().outcomes.a.status).toBe('success');
  });
  it('200业务失败保旧成功值，其他成功独立；无来源与失效项跳过', async () => {
    mocks.get.mockImplementation(async (id: string) =>
      id === 'c'
        ? { ...view(id), source: { kind: 'none', status: 'none' } }
        : id === 'd'
          ? { ...view(id), source: { kind: 'imported', status: 'needs_reimport' } }
          : view(id)
    );
    mocks.query.mockImplementation(async (id: string) =>
      id === 'b'
        ? {
            view: {
              ...result(id).view,
              query: { ...result(id).view.query, status: 'failed', error_code: 'usage_forbidden' },
              stale: true,
            },
            cancelled: false,
          }
        : result(id)
    );
    await render();
    await click('查询已绑定账号额度');
    expect(mocks.query).toHaveBeenCalledTimes(2);
    expect(coordinator.snapshot().outcomes).toMatchObject({
      a: { status: 'success' },
      b: { status: 'failed' },
      c: { status: 'skipped' },
      d: { status: 'skipped' },
    });
    expect(coordinator.snapshot().views.b.query.observed_at).toBe('2026-11-13T00:00:00Z');
    expect(text()).toContain('陈旧观测');
  });
  it('来源修订与删除使排队项和迟到响应失效', async () => {
    const pending = new Map<string, ReturnType<typeof deferred<ReturnType<typeof result>>>>();
    mocks.query.mockImplementation((id: string) => {
      const d = deferred<ReturnType<typeof result>>();
      pending.set(id, d);
      return d.promise;
    });
    await render();
    await click('查询已绑定账号额度');
    await act(async () => {
      coordinator.observe(view('a', 2));
      coordinator.setAccounts(['a', 'b', 'c']);
      for (const [id, d] of pending) d.resolve(result(id));
      await flush();
    });
    expect(mocks.query).toHaveBeenCalledTimes(3);
    expect(coordinator.snapshot().views.a.binding_revision).toBe(2);
    expect(coordinator.snapshot().views.a.query.status).toBe('never');
    expect(coordinator.snapshot().views.d).toBeUndefined();
    expect(coordinator.snapshot().outcomes.a.status).toBe('skipped');
    expect(coordinator.snapshot().outcomes.d.status).toBe('skipped');
  });
  it('定时默认关闭，开启立即一轮，30分钟边界再一轮', async () => {
    vi.useFakeTimers();
    await render();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query).not.toHaveBeenCalled();
    await act(async () => {
      renderer!.root.findByType(ToggleSwitch).props.onChange(true);
      await flush();
    });
    expect(mocks.query).toHaveBeenCalledTimes(4);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1799999);
    });
    expect(mocks.query).toHaveBeenCalledTimes(4);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(mocks.query).toHaveBeenCalledTimes(8);
    await act(async () => {
      renderer!.root.findByType(ToggleSwitch).props.onChange(false);
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query).toHaveBeenCalledTimes(8);
    expect(coordinator.snapshot().enabled).toBe(false);
  });
  it('慢轮次不叠加，关闭取消队列和在途且不重放', async () => {
    vi.useFakeTimers();
    const pending: ReturnType<typeof deferred<ReturnType<typeof result>>>[] = [];
    mocks.query.mockImplementation(() => {
      const d = deferred<ReturnType<typeof result>>();
      pending.push(d);
      return d.promise;
    });
    await render();
    await act(async () => {
      coordinator.setEnabled(true);
      await flush();
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query).toHaveBeenCalledTimes(3);
    await act(async () => {
      coordinator.setEnabled(false);
      pending.forEach((d, i) => d.resolve(result(['a', 'b', 'c'][i])));
      await flush();
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(mocks.query).toHaveBeenCalledTimes(3);
    expect(mocks.query.mock.calls.every((c) => c[4].aborted)).toBe(true);
    expect(
      Object.values(coordinator.snapshot().outcomes).every((v) => v.status === 'cancelled')
    ).toBe(true);
  });
  it('管理401停止认证与定时；200上游失败不误判管理失效', async () => {
    mocks.query.mockRejectedValue({ status: 401 });
    await render();
    await act(async () => {
      coordinator.setEnabled(true);
      await flush();
    });
    expect(coordinator.snapshot()).toMatchObject({
      enabled: false,
      authenticated: false,
      running: false,
    });
    expect(mocks.query).toHaveBeenCalledTimes(3);
    await click('查询已绑定账号额度');
    expect(mocks.query).toHaveBeenCalledTimes(3);
  });
  it('单条在批量读取快照期间完成，仍复用且不倒退成功观测', async () => {
    const single = deferred<ReturnType<typeof result>>(),
      slow = deferred<KiloQuotaView>();
    mocks.query.mockImplementation((id: string) =>
      id === 'a' ? single.promise : Promise.resolve(result(id))
    );
    await render(true);
    await click('查询一次额度');
    mocks.get.mockImplementation(async (id: string) => (id === 'd' ? slow.promise : view(id)));
    await click('查询已绑定账号额度');
    await act(async () => {
      single.resolve(result('a'));
      await flush();
      slow.resolve(view('d'));
      await flush();
    });
    expect(mocks.query.mock.calls.filter((call) => call[0] === 'a')).toHaveLength(1);
    expect(coordinator.snapshot().views.a.query.status).toBe('success');
    expect(coordinator.snapshot().outcomes.a.status).toBe('success');
  });
  it('批量先读取慢快照，随后单条完成也在本轮复用而不二次POST', async () => {
    const slow = deferred<KiloQuotaView>();
    await render(true);
    mocks.get.mockImplementation(async (id: string) => (id === 'd' ? slow.promise : view(id)));
    await click('查询已绑定账号额度');
    await click('查询一次额度');
    expect(mocks.query.mock.calls.filter((call) => call[0] === 'a')).toHaveLength(1);
    await act(async () => {
      slow.resolve(view('d'));
      await flush();
    });
    expect(mocks.query.mock.calls.filter((call) => call[0] === 'a')).toHaveLength(1);
    expect(coordinator.snapshot().outcomes.a.status).toBe('success');
    expect(coordinator.snapshot().views.a.query.status).toBe('success');
    await click('查询已绑定账号额度');
    expect(mocks.query.mock.calls.filter((call) => call[0] === 'a')).toHaveLength(2);
  });
  it('批量成功后面板旧GET迟到，不降回同版本旧观测', async () => {
    coordinator.setAccounts(['a']);
    const query = deferred<ReturnType<typeof result>>(),
      read = deferred<KiloQuotaView>();
    mocks.query.mockReturnValue(query.promise);
    await render(true);
    await click('查询已绑定账号额度');
    mocks.get.mockReturnValue(read.promise);
    await click('读取当前观测');
    await act(async () => {
      query.resolve(result('a'));
      await flush();
    });
    expect(coordinator.snapshot().views.a.query.status).toBe('success');
    await act(async () => {
      read.resolve(view('a'));
      await flush();
    });
    expect(coordinator.snapshot().views.a.query.status).toBe('success');
    expect(text()).toContain('2026-11-13T00:00:00Z');
    expect(text()).not.toContain('尚无成功查询');
  });
  it('快照已读但尚未入查询队列时改来源，旧快照也跳过', async () => {
    const slow = deferred<KiloQuotaView>();
    mocks.get.mockImplementation(async (id: string) => (id === 'd' ? slow.promise : view(id)));
    await render();
    await click('查询已绑定账号额度');
    await act(async () => {
      coordinator.invalidate('a');
      slow.resolve(view('d'));
      await flush();
    });
    expect(mocks.query.mock.calls.map((call) => call[0])).not.toContain('a');
    expect(coordinator.snapshot().outcomes.a.status).toBe('skipped');
  });
  it('来源保存尚未回包时显式批量不读取旧来源并重新排队', async () => {
    coordinator.observe(view('a'));
    coordinator.invalidate('a');
    await render();
    await click('查询已绑定账号额度');
    expect(mocks.get.mock.calls.map((call) => call[0])).not.toContain('a');
    expect(mocks.query.mock.calls.map((call) => call[0])).not.toContain('a');
    expect(coordinator.snapshot().outcomes.a.status).toBe('skipped');
  });
  it('停止后立即开启新轮，已取消pending结算为取消而非永久等待', async () => {
    coordinator.setAccounts(['a']);
    const pending = deferred<ReturnType<typeof result>>();
    mocks.query.mockReturnValue(pending.promise);
    await render();
    await click('查询已绑定账号额度');
    await act(async () => {
      coordinator.stop();
      await flush();
    });
    await click('查询已绑定账号额度');
    await act(async () => {
      pending.resolve(result('a'));
      await flush();
    });
    expect(mocks.query).toHaveBeenCalledTimes(1);
    expect(coordinator.snapshot().running).toBe(false);
    expect(coordinator.snapshot().outcomes.a.status).toBe('cancelled');
  });
  it('499安全视图保留但计取消，未知业务状态不计成功', async () => {
    coordinator.setAccounts(['a', 'b']);
    mocks.query.mockImplementation(async (id: string) =>
      id === 'a'
        ? {
            view: {
              ...result(id).view,
              query: { ...result(id).view.query, status: 'cancelled' },
              stale: true,
            },
            cancelled: true,
          }
        : { view: view(id), cancelled: false }
    );
    await render();
    await click('查询已绑定账号额度');
    expect(coordinator.snapshot().outcomes).toMatchObject({
      a: { status: 'cancelled' },
      b: { status: 'failed' },
    });
    expect(coordinator.snapshot().views.a.query.observed_at).not.toBeNull();
  });
  it.each(['account', 'connection', 'revision'])('拒绝%s不匹配的迟到结果', async (kind) => {
    coordinator.setAccounts(['a']);
    mocks.query.mockResolvedValue({
      view: {
        ...result('a').view,
        ...(kind === 'account'
          ? { account_id: 'other' }
          : kind === 'connection'
            ? { connection_ref: 'b'.repeat(32) }
            : { binding_revision: 2 }),
      },
      cancelled: false,
    });
    await render();
    await click('查询已绑定账号额度');
    expect(coordinator.snapshot().views.a.query.status).toBe('never');
    expect(coordinator.snapshot().outcomes.a.status).not.toBe('success');
  });
});
