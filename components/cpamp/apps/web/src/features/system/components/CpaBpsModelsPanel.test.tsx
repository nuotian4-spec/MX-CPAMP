import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CpaBpsModelsPanel } from './CpaBpsModelsPanel';

const mocks = vi.hoisted(() => ({ models: vi.fn(), list: vi.fn() }));
vi.mock('@/services/api/authFiles', () => ({
  authFilesApi: { list: mocks.list, getModelsWithDiscovery: mocks.models },
}));
vi.mock('@/services/api/plugins', () => ({
  pluginsApi: { getConfig: async () => ({ auto_discover: true, models: [] }) },
}));
let renderer: ReactTestRenderer | undefined;
afterEach(() => {
  act(() => renderer?.unmount());
});

describe('BPS 模型发现状态', () => {
  it.each([
    [{ status: 'failed', error_code: 'bps_model_discovery_rejected' }, [], '插件模型发现失败'],
    [{ status: 'succeeded' }, [], '最近模型发现成功，当前注册目录为空'],
    [{ status: 'succeeded' }, [{ id: 'model-a' }], '模型已登记'],
    [{ status: 'disabled' }, [], '凭证已停用'],
    [undefined, [{ id: 'previous-model' }], '尚无本次运行的模型发现记录'],
  ])('展示状态 %j', async (discovery, models, expected) => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    mocks.list.mockResolvedValue({ files: [{ id: 'a', name: 'new.json', provider: 'cpa-bps' }] });
    mocks.models.mockResolvedValue({ models, discovery });
    await act(async () => {
      renderer = create(
        <CpaBpsModelsPanel
          scope={{ apiBase: 'http://fixture.invalid', managementKey: 'synthetic' }}
        />
      );
    });
    expect(JSON.stringify(renderer?.toJSON())).toContain(expected);
    expect(mocks.models).toHaveBeenCalledWith('a', {
      apiBase: 'http://fixture.invalid',
      managementKey: 'synthetic',
    });
  });
});
