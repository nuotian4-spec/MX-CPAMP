import { describe, expect, it, vi } from 'vitest';
import { authFilesApi } from './authFiles';
import { bpsSettingsApi } from './bpsSettings';
import { loadBpsBoundAuthFiles } from './bpsCredentialSnapshot';
vi.mock('./authFiles', () => ({ authFilesApi: { list: vi.fn() } }));
vi.mock('./bpsSettings', () => ({ bpsSettingsApi: { get: vi.fn() } }));
const scope = { apiBase: 'http://panel.invalid', managementKey: 'TEST' };
describe('BPS绑定不阻断既有CPA列表', () => {
  it('BPS管理入口缺失时普通账号列表继续可读，BPS无引用', async () => {
    vi.mocked(bpsSettingsApi.get).mockRejectedValueOnce(new Error('404'));
    vi.mocked(authFilesApi.list).mockResolvedValue({
      files: [
        { name: 'codex.json', provider: 'codex' },
        { name: 'bps.json', provider: 'bps' },
      ],
    });
    const result = await loadBpsBoundAuthFiles(scope);
    expect(result.files).toHaveLength(2);
    expect(result.files.every((file) => !file.bps_connection_ref)).toBe(true);
  });
  it('读取过程中实例变化时拒绝把凭证绑定到新实例', async () => {
    const settings = {
      enabled: true,
      images_enabled: true,
      image_transport: 'native' as const,
      image_relay_base_url: '',
      limits: {},
    };
    vi.mocked(bpsSettingsApi.get)
      .mockResolvedValueOnce({ ...settings, connection_ref: 'old' })
      .mockResolvedValueOnce({ ...settings, connection_ref: 'new' });
    vi.mocked(authFilesApi.list).mockResolvedValueOnce({
      files: [{ name: 'bps.json', provider: 'bps', auth_index: 'one' }],
    });
    await expect(loadBpsBoundAuthFiles(scope)).rejects.toThrow('CPA连接已变更');
  });
});
