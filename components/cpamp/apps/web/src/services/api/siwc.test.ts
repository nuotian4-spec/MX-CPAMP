import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { siwcApi, siwcErrorKey } from './siwc';
const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'synthetic' };
const target = { auth_id: 'auth-a', auth_index: 'index-a', connection_ref: 'connection-a' };
const credential = {
  ...target,
  provider: 'codex',
  siwc_bind_supported: true,
  use_siwc: true,
  siwc_status: 'valid',
  siwc_bound: true,
};

describe('SIWC 连接与回执合同', () => {
  beforeEach(() => vi.clearAllMocks());
  it('同源 Manager 根路由和精确凭证、连接贯穿所有变更', async () => {
    client.get.mockResolvedValue(credential);
    await siwcApi.credential({ auth_id: target.auth_id, auth_index: target.auth_index }, scope);
    expect(client.get).toHaveBeenCalledWith(
      '/siwc/credential',
      expect.objectContaining({
        baseURL: 'https://panel.invalid/api',
        cpampScopedRequest: true,
        params: { auth_id: 'auth-a', auth_index: 'index-a' },
      })
    );
    client.post.mockResolvedValue({
      ...target,
      flow: 'siwc_bind',
      status: 'wait',
      state: 'state-issued',
      url: 'https://auth.openai.com/authorize?state=state-issued',
      expires_in: 300,
    });
    await siwcApi.bind(target, scope);
    expect(client.post).toHaveBeenCalledWith(
      '/siwc/bind',
      target,
      expect.objectContaining({ baseURL: 'https://panel.invalid/api' })
    );
    client.delete.mockResolvedValue({ status: 'cancelled', cancelled: true });
    await siwcApi.cancel(target, 'state-issued', scope);
    expect(client.delete).toHaveBeenCalledWith(
      '/siwc/session',
      expect.objectContaining({ params: { ...target, state: 'state-issued' } })
    );
  });
  it.each([
    'http://127.0.0.1:1455/auth/callback?state=state-issued&code=code-issued&client_id=issued-client',
    'http://127.0.0.1:1455/auth/callback?state=state-issued&error=access_denied',
  ])('完整回调原样传递且不替换签发 client_id 或拒绝 error', async (redirect_url) => {
    client.post.mockResolvedValue({ status: 'wait', submitted: true });
    await siwcApi.callback(target, 'state-issued', redirect_url, scope);
    expect(client.post).toHaveBeenCalledWith(
      '/siwc/callback',
      { ...target, state: 'state-issued', redirect_url },
      expect.any(Object)
    );
  });
  it('缺能力字段和旧接口明确不支持，不调用普通登录', async () => {
    client.get.mockResolvedValue({ ...credential, siwc_bind_supported: undefined });
    await expect(siwcApi.credential(target, scope)).rejects.toMatchObject({
      code: 'siwc_unsupported',
    });
    expect(siwcErrorKey({ status: 404 })).toBe('unsupported');
    expect(siwcErrorKey({ status: 501, data: { code: 'siwc_unsupported' } })).toBe('unsupported');
    expect(client.post).not.toHaveBeenCalled();
  });
  it('状态 ok 必须含同一连接和身份的回读', async () => {
    client.get.mockResolvedValue({ status: 'ok' });
    await expect(siwcApi.status(target, 's', scope)).rejects.toThrow();
    client.get.mockResolvedValue({
      status: 'ok',
      credential: { ...credential, connection_ref: 'other' },
    });
    await expect(siwcApi.status(target, 's', scope)).rejects.toMatchObject({
      code: 'siwc_identity_changed',
    });
    client.get.mockResolvedValue({ status: 'ok', credential });
    await expect(siwcApi.status(target, 's', scope)).resolves.toEqual({ status: 'ok', credential });
  });
  it('取消 false+wait 或 false+error 不借旧绑定摘要报成功', async () => {
    client.delete.mockResolvedValue({ status: 'wait', cancelled: false, credential });
    await expect(siwcApi.cancel(target, 's', scope)).resolves.toEqual({
      status: 'wait',
      cancelled: false,
    });
    client.delete.mockResolvedValue({
      status: 'error',
      code: 'siwc_session_failed',
      cancelled: false,
      credential,
    });
    await expect(siwcApi.cancel(target, 's', scope)).resolves.toEqual({
      status: 'error',
      code: 'siwc_session_failed',
      cancelled: false,
    });
    client.delete.mockResolvedValue({ status: 'ok', cancelled: false, credential });
    await expect(siwcApi.cancel(target, 's', scope)).resolves.toEqual({
      status: 'ok',
      cancelled: false,
      credential,
    });
  });
});
