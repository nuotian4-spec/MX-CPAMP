import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('./client', async (original) => ({
  ...(await original<object>()),
  apiClient: { get: mocks.get },
}));
import { pluginsApi } from './plugins';

beforeEach(() => {
  mocks.get.mockReset();
  mocks.get.mockResolvedValue({ auto_discover: true, models: [] });
});
describe('插件配置连接范围', () => {
  it('指定scope时配置读取绑定目标和管理认证', async () => {
    await pluginsApi.getConfig('cpa-bps', {
      apiBase: 'https://scope.invalid',
      managementKey: 'TEST_ONLY',
    });
    expect(mocks.get).toHaveBeenCalledWith(
      '/plugins/cpa-bps/config',
      expect.objectContaining({
        baseURL: expect.stringContaining('https://scope.invalid'),
        headers: { Authorization: 'Bearer TEST_ONLY' },
        cpampScopedRequest: true,
      })
    );
  });
  it('既有无scope调用保持单参数接口', async () => {
    await pluginsApi.getConfig('cpa-bps');
    expect(mocks.get).toHaveBeenCalledWith('/plugins/cpa-bps/config');
  });
});
