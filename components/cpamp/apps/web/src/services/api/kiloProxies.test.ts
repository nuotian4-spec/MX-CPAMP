import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import {
  kiloProxiesApi,
  kiloProxyError,
  kiloProxyDuplicate,
  type KiloProxyState,
} from './kiloProxies';
const scope = { apiBase: 'https://manager.invalid', managementKey: 'ADMIN_FIXTURE' };
const snapshot: KiloProxyState = {
  revision: 7,
  runtime_revision: 7,
  managed: true,
  proxy_limit: 10,
  proxies: [],
  recycle: [],
  accounts: [],
  connection_ref: 'a'.repeat(32),
};
describe('Kilo代理API', () => {
  beforeEach(() => vi.clearAllMocks());
  it('固定scope、取消signal与读到的版本/ref，无自动重试', () => {
    const signal = new AbortController().signal;
    const config = {
      baseURL: 'https://manager.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
      signal,
    };
    const version = { expected_revision: 7, connection_ref: snapshot.connection_ref };
    kiloProxiesApi.list(scope, signal);
    expect(client.get).toHaveBeenCalledWith('/kilo/proxies', config);
    kiloProxiesApi.create({ url: 'http://user:SECRET@127.0.0.1:80' }, snapshot, scope, signal);
    expect(client.post).toHaveBeenCalledWith(
      '/kilo/proxies',
      { url: 'http://user:SECRET@127.0.0.1:80', ...version },
      config
    );
    kiloProxiesApi.update('p 1', { name: '新名' }, snapshot, scope, signal);
    expect(client.patch).toHaveBeenCalledWith(
      '/kilo/proxies/p%201',
      { name: '新名', ...version },
      config
    );
    expect(client.patch.mock.calls[0][1]).not.toHaveProperty('url');
    kiloProxiesApi.capacity(12, snapshot, scope, signal);
    expect(client.patch).toHaveBeenLastCalledWith(
      '/kilo/proxy-pool',
      { proxy_limit: 12, ...version },
      config
    );
    kiloProxiesApi.egress(
      'a 1',
      { mode: 'direct', exclusive_proxy_ids: [], primary_proxy_id: '' },
      snapshot,
      scope,
      signal
    );
    expect(client.put).toHaveBeenCalledWith(
      '/kilo/accounts/a%201/egress',
      { mode: 'direct', exclusive_proxy_ids: [], primary_proxy_id: '', ...version },
      config
    );
    expect(client.post).toHaveBeenCalledTimes(1);
    expect(client.put).toHaveBeenCalledTimes(1);
  });
  it('回收站读取和四写固定路径、版本/ref、取消与显式清空确认', () => {
    const signal = new AbortController().signal;
    const config = {
      baseURL: 'https://manager.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
      signal,
    };
    const version = { expected_revision: 7, connection_ref: snapshot.connection_ref };
    kiloProxiesApi.recycle(scope, signal);
    expect(client.get).toHaveBeenCalledWith('/kilo/proxy-recycle', config);
    kiloProxiesApi.archive('p 1', snapshot, scope, signal);
    expect(client.delete).toHaveBeenCalledWith('/kilo/proxies/p%201', { ...config, data: version });
    kiloProxiesApi.restore('p 1', snapshot, scope, signal);
    expect(client.post).toHaveBeenCalledWith('/kilo/proxy-recycle/p%201/restore', version, config);
    kiloProxiesApi.recheck('p 1', snapshot, scope, signal);
    expect(client.post).toHaveBeenCalledWith('/kilo/proxies/p%201/recheck', version, config);
    kiloProxiesApi.clear(snapshot, scope, signal);
    expect(client.delete).toHaveBeenLastCalledWith('/kilo/proxy-recycle', {
      ...config,
      data: { ...version, confirm: true },
    });
    expect(client.post).toHaveBeenCalledTimes(2);
    expect(client.delete).toHaveBeenCalledTimes(2);
    expect(kiloProxyError({ status: 499 })).toContain('已取消');
  });
  it('冲突提示刷新后显式提交，错误文本不泄漏输入', () => {
    for (const status of [400, 401, 404, 409, 499, 503, 502])
      expect(kiloProxyError({ status, message: 'SECRET_URL' })).not.toContain('SECRET');
    expect(kiloProxyError({ status: 409 })).toContain('显式提交');
  });
});

it('预览只传URL与Manager连接护栏，不传revision或可信观测', () => {
  const signal = new AbortController().signal;
  kiloProxiesApi.preview('http://USER:SECRET@127.0.0.1:80', snapshot, scope, signal);
  expect(client.post).toHaveBeenCalledWith(
    '/kilo/proxies/preview',
    { url: 'http://USER:SECRET@127.0.0.1:80', connection_ref: snapshot.connection_ref },
    expect.objectContaining({ signal })
  );
});
it('明确重复读取data.error，非业务code及不安全附带字段不采纳', () => {
  expect(kiloProxyDuplicate({ status: 409, code: 'proxy_ip_already_registered' })).toBeNull();
  expect(
    kiloProxyDuplicate({ status: 409, data: { code: 'proxy_ip_already_registered' } })
  ).toBeNull();
  expect(
    kiloProxyDuplicate({
      status: 409,
      data: { error: 'proxy_ip_already_registered', ip: 'SECRET', proxy_id: 'http://SECRET' },
    })
  ).toEqual({ ip: undefined, proxy_id: undefined });
  expect(kiloProxyError({ status: 409, data: { error: 'proxy_ip_already_registered' } })).toContain(
    '重复条目未保存'
  );
});
