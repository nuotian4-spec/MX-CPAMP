import { beforeEach, describe, expect, it, vi } from 'vitest';
import { KiloAddFlow } from './kiloAddFlow';
import type { KiloQuotaBinding } from '@/services/api/kiloQuota';
import type { KiloDeviceSaved } from '@/services/api/kiloDevice';
const api = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), list: vi.fn() }));
const proxy = vi.hoisted(() => ({ list: vi.fn(), egress: vi.fn() }));
const quota = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), query: vi.fn() }));
vi.mock('@/services/api/kilo', () => ({ kiloApi: api }));
vi.mock('@/services/api/kiloProxies', () => ({ kiloProxiesApi: proxy }));
vi.mock('@/services/api/kiloQuota', () => ({ kiloQuotaApi: quota }));
const scope = { apiBase: 'http://127.0.0.1:13940', managementKey: 'SYNTHETIC' };
const ref = 'connection-a';
const id = 'saved-synthetic';
const desired = { mode: 'direct' as const, primary_proxy_id: '', exclusive_proxy_ids: [] };
const source: KiloQuotaBinding = {
  kind: 'cpa_codex',
  credential_id: 'source-a',
  chatgpt_account_id: 'outer-a',
};
const account = { id, enabled: false, connection_ref: ref };
const deviceSaved = {
  ...account,
  credential_saved: true,
  egress_ready: true,
  egress_mode: 'direct',
  egress_status: 'available',
  egress_reason: '',
} as KiloDeviceSaved;
const state = (applied = false) => ({
  connection_ref: ref,
  revision: 9,
  proxies: [],
  accounts: applied ? [{ account_id: id, ...desired }] : [],
});
const view = (bound = false) => ({
  account_id: id,
  binding_revision: bound ? 8 : 7,
  connection_ref: ref,
  source: bound
    ? { kind: 'cpa_codex', credential_id: 'source-a', status: 'ready' }
    : { kind: 'none', status: 'none' },
});
let current = true;
const make = (binding: KiloQuotaBinding | undefined = source, enabled = true) =>
  new KiloAddFlow(scope, ref, desired, binding, enabled, () => current, vi.fn());
beforeEach(() => {
  vi.resetAllMocks();
  current = true;
  api.create.mockResolvedValue(account);
  api.list.mockResolvedValue({ connection_ref: ref, accounts: [account] });
  api.update.mockResolvedValue({ ...account, enabled: true });
  proxy.list.mockResolvedValue(state());
  proxy.egress.mockResolvedValue(state(true));
  quota.get.mockResolvedValue(view());
  quota.put.mockResolvedValue(view(true));
});
describe('添加凭证分段保存', () => {
  it('先停用落盘，出口和稳定双ID来源成功后才启用，读取来源不查询', async () => {
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY', name: '合成', enabled: true });
    expect(api.create).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: false }),
      ref,
      scope
    );
    expect(quota.put).toHaveBeenCalledWith(id, source, 7, ref, scope);
    expect(api.create.mock.invocationCallOrder[0]).toBeLessThan(
      proxy.egress.mock.invocationCallOrder[0]
    );
    expect(proxy.egress.mock.invocationCallOrder[0]).toBeLessThan(
      quota.put.mock.invocationCallOrder[0]
    );
    expect(quota.put.mock.invocationCallOrder[0]).toBeLessThan(
      api.update.mock.invocationCallOrder[0]
    );
    expect(flow.progress.done).toBe(true);
    expect(quota.query).not.toHaveBeenCalled();
  });
  it('出口失败后先读回再定向恢复，不重建，不恢复启用授权', async () => {
    proxy.egress.mockRejectedValueOnce({ status: 409 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    expect(flow.progress).toMatchObject({ id, needsRead: true, egress: false });
    await flow.resume();
    expect(proxy.egress).toHaveBeenCalledTimes(1);
    await flow.read();
    flow.supplyMaterial(source);
    await flow.resume();
    expect(flow.progress).toMatchObject({ done: true, enabled: false });
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(proxy.egress).toHaveBeenCalledTimes(2);
    expect(api.update).not.toHaveBeenCalled();
  });
  it('关联失败后保留出口成功，只补来源且不覆盖外部人工停用', async () => {
    quota.put.mockRejectedValueOnce({ status: 503 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    expect(flow.progress).toMatchObject({ id, egress: true, source: false });
    await flow.read();
    flow.supplyMaterial(source);
    await flow.resume();
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(proxy.egress).toHaveBeenCalledTimes(1);
    expect(quota.put).toHaveBeenCalledTimes(2);
    expect(api.update).not.toHaveBeenCalled();
  });
  it('设备保存成功后关联失败独立恢复，不重发设备保存和出口', async () => {
    quota.put.mockRejectedValueOnce({ status: 409 });
    const flow = make();
    await flow.deviceSaved(id, deviceSaved);
    expect(flow.progress).toMatchObject({ id, egress: true, source: false, needsRead: true });
    await flow.read();
    flow.supplyMaterial(source);
    await flow.resume();
    expect(flow.progress.done).toBe(true);
    expect(api.create).not.toHaveBeenCalled();
    expect(proxy.egress).not.toHaveBeenCalled();
    expect(api.update).not.toHaveBeenCalled();
  });
  it('来源写入回执丢失，读回匹配版本后不重复关联', async () => {
    quota.put.mockRejectedValueOnce({ status: 499 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    quota.get.mockResolvedValue(view(true));
    await flow.read();
    await flow.resume();
    expect(flow.progress.done).toBe(true);
    expect(quota.put).toHaveBeenCalledTimes(1);
  });
  it('出口回执丢失后以真实出口读回确认，避免重发', async () => {
    proxy.egress.mockRejectedValueOnce({ status: 499 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    proxy.list.mockResolvedValue(state(true));
    await flow.read();
    flow.supplyMaterial(source);
    await flow.resume();
    expect(flow.progress.done).toBe(true);
    expect(proxy.egress).toHaveBeenCalledTimes(1);
  });
  it('连接变更保留已保存ID并停止后续写入', async () => {
    api.create.mockImplementation(async () => {
      current = false;
      return account;
    });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    expect(flow.progress.id).toBe(id);
    expect(proxy.egress).not.toHaveBeenCalled();
    await flow.read();
    expect(flow.progress.needsRead).toBe(true);
  });
  it('未知创建结果禁止再次create', async () => {
    api.create.mockRejectedValue({ status: 499 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(flow.progress.needsRead).toBe(true);
  });
  it('来源被外部改绑时保留当前来源，不盲目覆盖', async () => {
    quota.put.mockRejectedValueOnce({ status: 409 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    quota.get.mockResolvedValue({
      ...view(true),
      source: { kind: 'cpa_codex', credential_id: 'other', status: 'ready' },
    });
    await flow.read();
    await flow.resume();
    expect(flow.progress.needsRead).toBe(true);
    expect(quota.put).toHaveBeenCalledTimes(1);
  });
  it('用户选择停用或来源暂停时保持停用', async () => {
    const flow = make(source, false);
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    expect(api.update).not.toHaveBeenCalled();
    api.list.mockResolvedValue({
      connection_ref: ref,
      accounts: [{ ...account, source_policy: { paused: true } }],
    });
    await make().create({ api_key: 'SYNTHETIC_KEY' });
    expect(api.update).not.toHaveBeenCalled();
  });
  it('出口读回第三种外部配置时停止，保留人工配置', async () => {
    proxy.egress.mockRejectedValueOnce({ status: 409 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    proxy.list.mockResolvedValue({
      ...state(),
      accounts: [
        {
          account_id: id,
          mode: 'pool',
          primary_proxy_id: 'external',
          candidate_proxy_ids: ['external'],
        },
      ],
    });
    await flow.read();
    await flow.resume();
    expect(flow.progress.needsRead).toBe(true);
    expect(flow.progress.error).toContain('保留当前配置');
    expect(proxy.egress).toHaveBeenCalledTimes(1);
    expect(quota.put).not.toHaveBeenCalled();
  });
  it('读回到重试期间出口又被人工改变时仍保留外部配置', async () => {
    proxy.egress.mockRejectedValueOnce({ status: 409 });
    const flow = make();
    await flow.create({ api_key: 'SYNTHETIC_KEY' });
    await flow.read();
    expect(flow.progress.needsRead).toBe(false);
    proxy.list.mockResolvedValue({
      ...state(),
      accounts: [{ account_id: id, mode: 'auto', max_ips: 5 }],
    });
    flow.supplyMaterial(source);
    await flow.resume();
    expect(flow.progress.needsRead).toBe(true);
    expect(proxy.egress).toHaveBeenCalledTimes(1);
  });
  it.each(['storage_failed', 'poll_only'] as const)(
    '设备保存%s保留ID，先读出口再继续来源且不重发保存',
    async (kind) => {
      const flow = make();
      await flow.deviceSaved(
        id,
        kind === 'storage_failed'
          ? { ...deviceSaved, egress_ready: false, egress_error: 'storage_failed' }
          : undefined
      );
      expect(flow.progress).toMatchObject({ id, egress: false, needsRead: true });
      expect(quota.put).not.toHaveBeenCalled();
      proxy.list.mockResolvedValue(state(true));
      await flow.read();
      flow.supplyMaterial(source);
      await flow.resume();
      expect(flow.progress).toMatchObject({ done: true, egress: true });
      expect(api.create).not.toHaveBeenCalled();
      expect(proxy.egress).not.toHaveBeenCalled();
      expect(api.update).not.toHaveBeenCalled();
    }
  );
  it('设备配置已保存但等待容量时分开展示并继续绑定', async () => {
    const flow = make();
    await flow.deviceSaved(id, {
      ...deviceSaved,
      egress_ready: false,
      egress_error: 'capacity_wait',
      egress_status: 'pending',
    });
    expect(flow.progress).toMatchObject({ id, egress: true, egressWaiting: true, done: true });
    expect(quota.put).toHaveBeenCalledTimes(1);
    expect(proxy.egress).not.toHaveBeenCalled();
  });
});
