import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ getRaw: vi.fn(), put: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: mocks,
  createScopedApiRequestConfig: (scope: { apiBase: string; managementKey: string }) => ({
    baseURL: `${scope.apiBase}/v0/management`,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
    cpampScopedRequest: true,
  }),
}));
import { configFileApi } from './configFile';

describe('配置YAML事务传输', () => {
  beforeEach(() => vi.clearAllMocks());
  it('保存完整YAML并传递固定管理连接、取消信号与Core版本条件', async () => {
    const yaml =
      'config-version: 8\naccess: {api-keys: [synthetic], client-key-scopes: [{api-key: synthetic, channel: kilo}]}\n';
    const scope = { apiBase: 'http://manager.invalid', managementKey: 'synthetic-admin' };
    const signal = new AbortController().signal;
    const connectionRef = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
    mocks.getRaw.mockResolvedValue({
      data: yaml,
      headers: { etag: '"version"', 'x-cpa-connection-ref': connectionRef },
    });
    const snapshot = await configFileApi.fetchSnapshot({ scope, signal });
    expect(snapshot).toEqual({ yaml, etag: '"version"', connectionRef });
    await configFileApi.saveConfigYaml(yaml, { scope, signal, etag: snapshot.etag, connectionRef });
    expect(mocks.put).toHaveBeenCalledExactlyOnceWith(
      '/config.yaml',
      yaml,
      expect.objectContaining({
        baseURL: 'http://manager.invalid/v0/management',
        signal,
        cpampScopedRequest: true,
        headers: expect.objectContaining({
          Authorization: 'Bearer synthetic-admin',
          'If-Match': '"version"',
          'X-CPA-Connection-Ref': connectionRef,
          'Content-Type': 'application/yaml',
        }),
      })
    );
  });
  it('不把非文本响应转成可提交配置', async () => {
    mocks.getRaw.mockResolvedValue({ data: { error: 'failed' }, headers: {} });
    await expect(configFileApi.fetchSnapshot()).rejects.toThrow('配置响应格式错误');
    expect(mocks.put).not.toHaveBeenCalled();
  });
});
