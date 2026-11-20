import { beforeEach, describe, expect, it, vi } from 'vitest';
import { authFilesApi } from './authFiles';
import { apiClient } from './client';

describe('凭证模型 v8 查询', () => {
  beforeEach(() => vi.restoreAllMocks());
  it('保留模型与发现状态，不暴露任意错误正文', async () => {
    vi.spyOn(apiClient, 'getV8').mockResolvedValue({
      models: [{ id: 'previous' }],
      discovery: { status: 'failed', error_code: 'SECRET', upstream_status: 502 },
    });
    const result = await authFilesApi.getModelsWithDiscovery('new.json');
    expect(result.models).toEqual([{ id: 'previous' }]);
    expect(result.discovery?.error_code).toBe('plugin_model_discovery_failed');
    expect(JSON.stringify(result)).not.toContain('SECRET');
    expect(JSON.stringify(result)).not.toContain('502');
  });
  it('只有404回退旧目录GET，状态仍未知', async () => {
    const request = vi.spyOn(apiClient, 'getV8').mockRejectedValue({ status: 404 });
    const legacy = vi
      .spyOn(authFilesApi, 'getModelsForAuthFile')
      .mockResolvedValue([{ id: 'old' }]);
    expect(await authFilesApi.getModelsWithDiscovery('a')).toEqual({ models: [{ id: 'old' }] });
    expect(legacy).toHaveBeenCalledTimes(1);
    request.mockRejectedValue({ status: 401 });
    await expect(authFilesApi.getModelsWithDiscovery('a')).rejects.toEqual({ status: 401 });
    expect(legacy).toHaveBeenCalledTimes(1);
  });
});
