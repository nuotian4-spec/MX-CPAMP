import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { ConcurrencySnapshot } from '@/services/api/credentialConcurrency';
import { useCredentialConcurrencySnapshot } from './useCredentialConcurrencySnapshot';

const mocks = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock('@/services/api/credentialConcurrency', () => ({ credentialConcurrencyApi: mocks }));
const scopeA = { apiBase: 'http://combined-a.invalid', managementKey: 'synthetic' };
const scopeB = { apiBase: 'http://combined-b.invalid', managementKey: 'synthetic' };
const snapshot = (ref: string): ConcurrencySnapshot => ({
  mode: 'local', connection_ref: ref, default_limit: 20, pool_waiting: 0, credentials: [],
});
function deferred() {
  let resolve!: (value: ConcurrencySnapshot) => void;
  const promise = new Promise<ConcurrencySnapshot>((done) => { resolve = done; });
  return { promise, resolve };
}

describe('组合共享并发快照合同', () => {
  let renderer: ReactTestRenderer | undefined;
  let state: ReturnType<typeof useCredentialConcurrencySnapshot>;
  function Probe({ scope }: { scope: ApiClientRequestScope }) {
    state = useCredentialConcurrencySnapshot(scope);
    return null;
  }
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    mocks.list.mockReset().mockResolvedValue(snapshot('ref-a'));
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer!.unmount());
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => { renderer = create(<Probe scope={scopeA} />); });
  }
  it('load返回本次确认快照并发布同scope新连接引用', async () => {
    await render();
    const next = snapshot('ref-b');
    mocks.list.mockResolvedValueOnce(next);
    let returned: ConcurrencySnapshot | undefined;
    await act(async () => { returned = await state.load(); });
    expect(returned).toBe(next);
    expect(state.snapshot).toBe(next);
  });
  it('连接切换清空旧快照且迟到load拒绝旧回执', async () => {
    await render();
    const old = deferred();
    const fresh = deferred();
    mocks.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    let failure: Promise<unknown>;
    await act(async () => { failure = state.load().catch((error: unknown) => error); });
    const oldSignal = mocks.list.mock.calls[1][1] as AbortSignal;
    await act(async () => { renderer!.update(<Probe scope={scopeB} />); });
    expect(oldSignal.aborted).toBe(true);
    expect(state.snapshot).toBeUndefined();
    await act(async () => { fresh.resolve(snapshot('ref-b')); });
    let error: unknown;
    await act(async () => { old.resolve(snapshot('ref-a')); error = await failure!; });
    expect(error).toEqual(new Error('并发目标已变化，请刷新后核对'));
    expect(state.snapshot).toEqual(snapshot('ref-b'));
  });
  it('后发load使前请求失效且保留最后确认结果', async () => {
    await render();
    const old = deferred();
    mocks.list.mockReturnValueOnce(old.promise).mockResolvedValueOnce(snapshot('ref-new'));
    let failure: Promise<unknown>;
    await act(async () => { failure = state.load().catch((error: unknown) => error); });
    await act(async () => { await state.load(); });
    let error: unknown;
    await act(async () => { old.resolve(snapshot('ref-old')); error = await failure!; });
    expect(error).toBeInstanceOf(Error);
    expect(state.snapshot).toEqual(snapshot('ref-new'));
  });
  it('当前读取失败返回异常但不覆盖已确认快照', async () => {
    await render();
    const failure = new Error('合成读取失败');
    mocks.list.mockRejectedValueOnce(failure);
    let error: unknown;
    await act(async () => { error = await state.load().catch((value: unknown) => value); });
    expect(error).toBe(failure);
    expect(state.snapshot).toEqual(snapshot('ref-a'));
    expect(state.error).toBe('并发状态暂不可读，请稍后刷新');
  });
});
