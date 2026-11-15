import { beforeEach, expect, it, vi } from 'vitest';
import { bpsResetCreditsApi } from '@/services/api/bpsResetCredits';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { getBpsResetCreditsKey, refreshBpsResetCredits } from './bpsResetCreditsState';
vi.mock('@/services/api/bpsResetCredits', () => ({ bpsResetCreditsApi: { query: vi.fn() } }));
const file = { name: 'fixture.json', id: 'id', auth_index: 'idx', provider: 'bps' };
const scope = { apiBase: 'https://fixture.invalid', managementKey: 'test' };
const result = {
  provider: 'bps' as const,
  auth_id: 'id',
  auth_index: 'idx',
  source: 'chatgpt_account_reset_credits' as const,
  query_mode: 'direct' as const,
  queried_at: '2026-11-15T00:00:00Z',
  count: 0,
  status: 'known' as const,
  error: null,
};
beforeEach(() => {
  useQuotaStore.getState().clearQuotaCache();
  vi.mocked(bpsResetCreditsApi.query).mockReset();
});
it('同目标只查询一次，不合并不同凭证', async () => {
  let finish!: (value: typeof result) => void;
  vi.mocked(bpsResetCreditsApi.query).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const a = refreshBpsResetCredits(file, scope, 'ref');
  const b = refreshBpsResetCredits(file, scope, 'ref');
  const c = refreshBpsResetCredits({ ...file, id: 'another' }, scope, 'ref');
  expect(a).toBe(b);
  expect(c).not.toBe(a);
  expect(bpsResetCreditsApi.query).toHaveBeenCalledTimes(2);
  finish(result);
  await Promise.all([a, c]);
});
it('连接切换及迟到回执不写入新generation', async () => {
  let finish!: (value: typeof result) => void;
  vi.mocked(bpsResetCreditsApi.query).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const pending = refreshBpsResetCredits(file, scope, 'ref');
  useQuotaStore.getState().activateQuotaCacheScope('new');
  finish(result);
  await pending;
  expect(useQuotaStore.getState().bpsResetCredits).toEqual({});
});
it('失败保留旧0及旧时间，不抹掉综合额度', async () => {
  vi.mocked(bpsResetCreditsApi.query)
    .mockResolvedValueOnce(result)
    .mockRejectedValueOnce(new Error('SECRET'));
  await refreshBpsResetCredits(file, scope, 'ref');
  await refreshBpsResetCredits(file, scope, 'ref');
  const state = useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)];
  expect(state).toMatchObject({ status: 'error', lastKnown: result });
  expect(state.error).not.toContain('SECRET');
  expect(
    useQuotaStore.persist.getOptions().partialize!(useQuotaStore.getState())
  ).not.toHaveProperty('bpsResetCredits');
});

it('详情卸载丢弃迟到结果并解除loading，换连接引用不借用旧次数', async () => {
  let finish!: (value: typeof result) => void;
  let active = true;
  vi.mocked(bpsResetCreditsApi.query).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  const pending = refreshBpsResetCredits(file, scope, 'ref', () => active);
  active = false;
  finish(result);
  await pending;
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)]).toBeUndefined();
  vi.mocked(bpsResetCreditsApi.query)
    .mockResolvedValueOnce(result)
    .mockRejectedValueOnce(new Error('offline'));
  await refreshBpsResetCredits(file, scope, 'ref');
  await refreshBpsResetCredits(file, scope, 'new-ref');
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)]).toMatchObject({
    status: 'error',
    connectionRef: 'new-ref',
  });
  expect(
    useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)].lastKnown
  ).toBeUndefined();
});

it.each(['成功', '失败'])('生命周期：旧详情%s回执不清掉新连接请求', async (outcome) => {
  let finish!: (value: typeof result) => void;
  let fail!: (e: Error) => void;
  let newFinish!: (value: typeof result) => void;
  let active = true;
  vi.mocked(bpsResetCreditsApi.query)
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
  const old = refreshBpsResetCredits(file, scope, 'old', () => active);
  active = false;
  const next = refreshBpsResetCredits(file, scope, 'new');
  if (outcome === '成功') finish(result);
  else fail(new Error('offline'));
  await old;
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)]).toMatchObject({
    status: 'loading',
    connectionRef: 'new',
  });
  newFinish(result);
  await next;
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)].status).toBe(
    'known'
  );
});
it('生命周期：失败卸载清理后回同账号可再次查询', async () => {
  let fail!: (e: Error) => void;
  let active = true;
  vi.mocked(bpsResetCreditsApi.query).mockReturnValueOnce(
    new Promise((_resolve, reject) => {
      fail = reject;
    })
  );
  const request = refreshBpsResetCredits(file, scope, 'ref', () => active);
  active = false;
  fail(new Error('offline'));
  await request;
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)]).toBeUndefined();
  vi.mocked(bpsResetCreditsApi.query).mockResolvedValueOnce(result);
  await refreshBpsResetCredits(file, scope, 'ref');
  expect(useQuotaStore.getState().bpsResetCredits[getBpsResetCreditsKey(file)].status).toBe(
    'known'
  );
});
