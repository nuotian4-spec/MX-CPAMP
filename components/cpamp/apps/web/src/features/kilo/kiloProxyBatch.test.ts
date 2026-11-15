import { beforeEach, describe, expect, it, vi } from 'vitest';
import { importKiloProxyRows, parseKiloProxyLines, previewKiloProxyRows } from './kiloProxyBatch';
import type { KiloProxyState } from '@/services/api/kiloProxies';
const api = vi.hoisted(() => ({ create: vi.fn(), preview: vi.fn(), list: vi.fn() }));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: api,
}));
const scope = { apiBase: 'http://127.0.0.1:10740', managementKey: 'SYNTHETIC' };
const state = {
  revision: 3,
  runtime_revision: 1,
  connection_ref: 'local-a',
  managed: true,
  proxy_limit: 10,
  proxies: [],
  recycle: [],
  accounts: [],
} as KiloProxyState;
beforeEach(() => {
  vi.resetAllMocks();
  api.create.mockImplementation(async (_input, previous: KiloProxyState) => ({
    ...previous,
    revision: previous.revision + 1,
    proxies: [...previous.proxies, { id: `p${previous.revision + 1}` }],
  }));
});
describe('多行代理解析与串行保存', () => {
  it('忽略空行、四种协议、完整URL批内去重，不把不同认证当同一入口', () => {
    const preview = parseKiloProxyLines(
      '\nhttp://USER:SECRET@192.0.2.1:8080\nhttp://USER:SECRET@192.0.2.1:8080\nhttp://USER:OTHER@192.0.2.1:8080\nhttps://192.0.2.2\nsocks5://192.0.2.3:1080\nsocks5h://192.0.2.4:1080\ninvalid SECRET\n'
    );
    expect(preview.rows.map((r) => r.status)).toEqual([
      'valid',
      'duplicate',
      'valid',
      'valid',
      'valid',
      'valid',
      'error',
    ]);
    expect(preview.inputs.size).toBe(5);
    expect(JSON.stringify(preview.rows)).not.toMatch(/SECRET|OTHER|USER/);
    expect(preview.rows[0]).toMatchObject({ line: 2, display: 'http://***@192.0.2.1:8080' });
  });
  it.each(['ftp://host:21', 'http://host:99999', 'socks5://host:0', 'http://', 'not-a-url'])(
    '错误行不回显材料：%s',
    (value) => {
      expect(parseKiloProxyLines(value).rows[0]).toMatchObject({
        status: 'error',
        display: '格式错误（原文已隐藏）',
      });
    }
  );
  it('保留已支持URL附加部分且预览隐藏可能携带的材料', () => {
    const result = parseKiloProxyLines(
      'http://user:PASS@192.0.2.1:8080/path?token=SECRET#fragment'
    );
    expect(result.rows[0].status).toBe('valid');
    expect(result.inputs.get(1)).toContain('token=SECRET');
    expect(JSON.stringify(result.rows)).not.toMatch(/PASS|SECRET|fragment|path/);
  });
  it('每行使用上一真实回包revision并捕获新增ID，提交后清材料', async () => {
    const preview = parseKiloProxyLines('http://192.0.2.1:80\nhttp://192.0.2.2:80');
    const changed = vi.fn();
    const result = await importKiloProxyRows({
      inputs: preview.inputs,
      snapshot: state,
      scope,
      signal: new AbortController().signal,
      current: () => true,
      changed,
      saved: vi.fn(),
    });
    expect(api.create.mock.calls.map((c) => c[1].revision)).toEqual([3, 4]);
    expect(result.snapshot.revision).toBe(5);
    expect(changed).toHaveBeenCalledWith(2, expect.objectContaining({ status: 'saved', id: 'p5' }));
    expect(preview.inputs.size).toBe(0);
    expect(result.uncertain).toBe(false);
  });
  it('第二行409立即停止第三行，第一行成功不重放', async () => {
    api.create
      .mockImplementationOnce(async () => ({
        ...state,
        revision: 4,
        proxies: [{ id: 'saved-one' }],
      }))
      .mockRejectedValueOnce({ status: 409, message: 'SECRET' });
    const preview = parseKiloProxyLines(
      'http://192.0.2.1:80\nhttp://192.0.2.2:80\nhttp://192.0.2.3:80'
    );
    const changed = vi.fn();
    const result = await importKiloProxyRows({
      inputs: preview.inputs,
      snapshot: state,
      scope,
      signal: new AbortController().signal,
      current: () => true,
      changed,
      saved: vi.fn(),
    });
    expect(api.create).toHaveBeenCalledTimes(2);
    expect(result).toMatchObject({ uncertain: true, snapshot: { revision: 4 } });
    expect(changed).toHaveBeenCalledWith(2, expect.objectContaining({ status: 'unknown' }));
    expect(changed.mock.calls.some(([line]) => line === 3)).toBe(false);
    expect(JSON.stringify(changed.mock.calls)).not.toContain('SECRET');
    expect(preview.inputs.size).toBe(0);
  });
  it.each(['abort', 'connection', 'identity'])('%s结果不明停止后续，不凭地址确认', async (kind) => {
    const abort = new AbortController();
    api.create.mockImplementationOnce(async () => {
      if (kind === 'abort') abort.abort();
      return {
        ...state,
        revision: 4,
        connection_ref: kind === 'connection' ? 'next' : state.connection_ref,
        proxies: kind === 'identity' ? [] : [{ id: 'p4' }],
      };
    });
    const preview = parseKiloProxyLines('http://192.0.2.1:80\nhttp://192.0.2.2:80');
    const result = await importKiloProxyRows({
      inputs: preview.inputs,
      snapshot: state,
      scope,
      signal: abort.signal,
      current: () => true,
      changed: vi.fn(),
      saved: vi.fn(),
    });
    expect(result.uncertain).toBe(true);
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(preview.inputs.size).toBe(0);
  });
});

describe('公网IP准入批量链', () => {
  it('服务器逐行观测、规范IP批内去重，预览不创建', async () => {
    api.preview
      .mockResolvedValueOnce({
        status: 'ready',
        reason: '',
        observed_exit_ip: '203.0.113.10',
        observed_at: '2026-11-15T00:00:00Z',
      })
      .mockResolvedValueOnce({
        status: 'ready',
        reason: '',
        observed_exit_ip: '203.0.113.10',
        observed_at: '2026-11-15T00:00:00Z',
      })
      .mockResolvedValueOnce({
        status: 'pending',
        reason: 'probe_failed',
        observed_exit_ip: null,
        observed_at: null,
      });
    const preview = parseKiloProxyLines('http://one:80\nhttp://two:80\nhttp://three:80');
    expect(
      await previewKiloProxyRows({
        preview,
        snapshot: state,
        scope,
        signal: new AbortController().signal,
        current: () => true,
        changed: vi.fn(),
      })
    ).toBe(true);
    expect(preview.rows.map((r) => r.status)).toEqual(['ready', 'duplicate', 'review']);
    expect([...preview.inputs.keys()]).toEqual([1, 3]);
    expect(api.create).not.toHaveBeenCalled();
  });
  it.each(['abort', 'connection', '409'])('预览%s停止后续，不产生可提交材料', async (kind) => {
    const controller = new AbortController();
    let current = true;
    api.preview.mockImplementationOnce(async () => {
      if (kind === 'abort') controller.abort();
      if (kind === 'connection') current = false;
      if (kind === '409') throw { status: 409 };
      return {
        status: 'ready',
        reason: '',
        observed_exit_ip: '203.0.113.10',
        observed_at: '2026-11-15T00:00:00Z',
      };
    });
    const preview = parseKiloProxyLines('http://one:80\nhttp://two:80');
    expect(
      await previewKiloProxyRows({
        preview,
        snapshot: state,
        scope,
        signal: controller.signal,
        current: () => current,
        changed: vi.fn(),
      })
    ).toBe(false);
    expect(api.preview).toHaveBeenCalledTimes(1);
    expect(preview.inputs.size).toBe(0);
    expect(api.create).not.toHaveBeenCalled();
  });
  it('只有error明确重复才拒绝该行并沿最新版本继续，待核已保存与未执行分开', async () => {
    api.create
      .mockRejectedValueOnce({
        status: 409,
        data: {
          error: 'proxy_ip_already_registered',
          ip: '203.0.113.10',
          proxy_id: 'kilo-proxy-original',
        },
      })
      .mockResolvedValueOnce({
        ...state,
        revision: 4,
        proxies: [{ id: 'pending', admission_status: 'pending', admission_reason: 'probe_failed' }],
      });
    const preview = parseKiloProxyLines('http://one:80\nhttp://two:80');
    const changed = vi.fn();
    const result = await importKiloProxyRows({
      inputs: preview.inputs,
      snapshot: state,
      scope,
      signal: new AbortController().signal,
      current: () => true,
      changed,
      saved: vi.fn(),
    });
    expect(api.create.mock.calls.map((c) => c[1].revision)).toEqual([3, 3]);
    expect(changed).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ status: 'rejected', registeredProxyID: 'kilo-proxy-original' })
    );
    expect(changed).toHaveBeenCalledWith(
      2,
      expect.objectContaining({ status: 'saved_pending', id: 'pending' })
    );
    expect(result.uncertain).toBe(false);
  });
  it('未知409停止并只读回看，不重放成功或结果不明行', async () => {
    api.create.mockRejectedValueOnce({
      status: 409,
      data: { code: 'proxy_ip_already_registered', error: 'other' },
    });
    api.list.mockResolvedValue(state);
    const preview = parseKiloProxyLines('http://one:80\nhttp://two:80');
    const result = await importKiloProxyRows({
      inputs: preview.inputs,
      snapshot: state,
      scope,
      signal: new AbortController().signal,
      current: () => true,
      changed: vi.fn(),
      saved: vi.fn(),
    });
    expect(result.uncertain).toBe(true);
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.list).toHaveBeenCalledTimes(1);
  });
});
