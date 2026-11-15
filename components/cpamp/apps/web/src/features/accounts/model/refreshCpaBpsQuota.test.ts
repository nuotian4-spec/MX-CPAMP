import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { refreshCpaBpsQuota } from './refreshCpaBpsQuota';
import { getCpaBpsQuotaKey } from './cpaBpsQuotaState';
vi.mock('@/services/api/cpaBpsQuota', () => ({ cpaBpsQuotaApi: { query: vi.fn() } }));
const file = { name: 'plugin.json', id: 'runtime-a', provider: 'cpa-bps', auth_index: 'index-a' };
const scope = { apiBase: 'http://a.invalid', managementKey: 'test-key' };
const result = {
  provider: 'cpa-bps' as const,
  source: 'cpa_bps_plugin_quota' as const,
  auth_index: 'index-a',
  auth_id: 'runtime-a',
  queried_at: '2026-11-12T01:00:00Z',
  quota: { subscription: null, summary: [], groups: [] },
};
beforeEach(() => {
  useQuotaStore.getState().clearQuotaCache();
  vi.mocked(cpaBpsQuotaApi.query).mockReset();
});
describe('插件额度共享查询', () => {
  it('同身份并发只查询一次，结果只存内存', async () => {
    let finish!: (value: typeof result) => void;
    vi.mocked(cpaBpsQuotaApi.query).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      })
    );
    const first = refreshCpaBpsQuota(file, scope);
    const second = refreshCpaBpsQuota(file, scope);
    expect(first).toBe(second);
    finish(result);
    await first;
    expect(cpaBpsQuotaApi.query).toHaveBeenCalledTimes(1);
    expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)].result).toEqual(result);
    const persisted = useQuotaStore.persist.getOptions().partialize!(useQuotaStore.getState());
    expect(persisted).not.toHaveProperty('cpaBpsQuota');
  });
  it('A到B再到A丢弃旧generation的回执', async () => {
    useQuotaStore.getState().activateQuotaCacheScope('A');
    let finish!: (value: typeof result) => void;
    vi.mocked(cpaBpsQuotaApi.query).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      })
    );
    const first = refreshCpaBpsQuota(file, scope);
    useQuotaStore.getState().activateQuotaCacheScope('B');
    useQuotaStore.getState().activateQuotaCacheScope('A');
    finish(result);
    expect(await first).toEqual({ status: 'ignored' });
    expect(useQuotaStore.getState().cpaBpsQuota).toEqual({});
  });
  it('失败保留旧快照及原查询时间', async () => {
    vi.mocked(cpaBpsQuotaApi.query)
      .mockResolvedValueOnce(result)
      .mockRejectedValueOnce(new Error('offline'));
    await refreshCpaBpsQuota(file, scope);
    await refreshCpaBpsQuota(file, scope);
    expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)]).toMatchObject({
      status: 'error',
      lastQueriedAt: result.queried_at,
    });
    expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)].result).toEqual(result);
  });
});

it('详情卸载后移除悬空loading，重新打开仍可查询', async () => {
  let finish!: (value: typeof result) => void;
  let active = true;
  vi.mocked(cpaBpsQuotaApi.query).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const pending = refreshCpaBpsQuota(file, scope, () => active);
  active = false;
  finish(result);
  await pending;
  expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)]).toBeUndefined();
});

it.each(['成功', '失败'])('生命周期：插件旧详情%s回执不清掉新连接请求', async (outcome) => {
  let finish!: (value: typeof result) => void;
  let fail!: (e: Error) => void;
  let newFinish!: (value: typeof result) => void;
  let active = true;
  vi.mocked(cpaBpsQuotaApi.query)
    .mockReturnValueOnce(
      new Promise((resolve, reject) => {
        finish = resolve;
        fail = reject;
      })
    )
    .mockReturnValueOnce(
      new Promise((resolve) => {
        newFinish = resolve;
      })
    );
  const old = refreshCpaBpsQuota(file, scope, () => active);
  active = false;
  const next = refreshCpaBpsQuota(file, { ...scope, managementKey: 'new' });
  if (outcome === '成功') finish(result);
  else fail(new Error('offline'));
  await old;
  expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)]).toMatchObject({
    status: 'loading',
  });
  newFinish(result);
  await next;
  expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)].status).toBe('success');
});
it('生命周期：插件失败卸载清理后可再次查询', async () => {
  let fail!: (e: Error) => void;
  let active = true;
  vi.mocked(cpaBpsQuotaApi.query).mockReturnValueOnce(
    new Promise((_resolve, reject) => {
      fail = reject;
    })
  );
  const request = refreshCpaBpsQuota(file, scope, () => active);
  active = false;
  fail(new Error('offline'));
  await request;
  expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)]).toBeUndefined();
  vi.mocked(cpaBpsQuotaApi.query).mockResolvedValueOnce(result);
  await refreshCpaBpsQuota(file, scope);
  expect(useQuotaStore.getState().cpaBpsQuota[getCpaBpsQuotaKey(file)].status).toBe('success');
});
