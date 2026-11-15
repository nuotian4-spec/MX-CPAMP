import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ post: vi.fn(), delete: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { kiloDeviceApi, kiloDeviceError, type KiloDeviceInput } from './kiloDevice';
const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN_FIXTURE' };
const reference = 'a'.repeat(32);
describe('Kilo设备授权API', () => {
  beforeEach(() => vi.clearAllMocks());
  it('四动作固定路径、编码session、显式scope/ref及可取消signal', () => {
    const signal = new AbortController().signal;
    const config = {
      baseURL: 'https://panel.invalid/api',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN_FIXTURE' },
      signal,
    };
    const input: KiloDeviceInput = {
      name: '合成账号',
      priority: 0,
      enabled: true,
      concurrency_limit: null,
      allow_direct_when_proxy_full: false,
      authorization_egress: { mode: 'direct' },
    };
    kiloDeviceApi.start(input, reference, scope, signal);
    expect(client.post).toHaveBeenNthCalledWith(
      1,
      '/kilo/device-auth',
      { ...input, connection_ref: reference },
      config
    );
    kiloDeviceApi.poll('session id', reference, scope, signal);
    expect(client.post).toHaveBeenNthCalledWith(
      2,
      '/kilo/device-auth/session%20id/poll',
      { connection_ref: reference },
      config
    );
    kiloDeviceApi.save('session id', reference, scope, signal);
    expect(client.post).toHaveBeenNthCalledWith(
      3,
      '/kilo/device-auth/session%20id/save',
      { connection_ref: reference },
      config
    );
    kiloDeviceApi.cancel('session id', reference, scope, signal);
    expect(client.delete).toHaveBeenCalledWith('/kilo/device-auth/session%20id', {
      ...config,
      data: { connection_ref: reference },
    });
  });
  it('错误仅按状态显示，不按正文判断成功或泄露私密材料', () => {
    for (const status of [400, 401, 403, 404, 409, 429, 499, 502, 503, undefined]) {
      const message = kiloDeviceError({
        status,
        message: 'SECRET_TOKEN saved=true',
        data: { token: 'SECRET_TOKEN' },
      });
      expect(message).not.toContain('SECRET_TOKEN');
      expect(message).not.toContain('已保存');
    }
    expect(kiloDeviceError({ status: 499 })).toContain('不代表服务端已取消');
    expect(kiloDeviceError({ status: 503 })).toContain('结果待核');
  });
});
