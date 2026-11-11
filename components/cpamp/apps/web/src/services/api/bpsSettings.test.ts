import { describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import { bpsSettingsApi } from './bpsSettings';
describe('BPS设置的管理认证与连接绑定', () => {
  it('通过Manager固定入口发送四字段设置与连接引用', () => {
    const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'MANAGER_ADMIN' };
    const input = { enabled: true, images_enabled: false, image_transport: 'native' as const, image_relay_base_url: '', connection_ref: 'a'.repeat(32) };
    bpsSettingsApi.save(input, scope);
    expect(client.put).toHaveBeenCalledWith('/bps/settings', input, {
      baseURL: 'https://panel.invalid/api', cpampScopedRequest: true,
      headers: { Authorization: 'Bearer MANAGER_ADMIN' },
    });
  });
});
