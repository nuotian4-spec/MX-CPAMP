import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BPSSettingsCard } from './BPSSettingsCard';

const mocks = vi.hoisted(() => ({
  auth: { apiBase: 'https://panel.invalid', managementKey: 'TEST_ONLY' },
  capabilities: vi.fn(),
  list: vi.fn(),
  models: vi.fn(),
  config: vi.fn(),
  oldSettings: vi.fn(),
  oldSave: vi.fn(),
  oldModels: vi.fn(),
  oldModelsSave: vi.fn(),
  access: vi.fn(),
}));
vi.mock('@/stores', () => ({
  useAuthStore: (select: (state: typeof mocks.auth) => unknown) => select(mocks.auth),
}));
vi.mock('@/services/api/loginTasks', () => ({
  loginTasksApi: { capabilities: mocks.capabilities },
}));
vi.mock('@/services/api/authFiles', () => ({
  authFilesApi: { list: mocks.list, getModelsForAuthFile: mocks.models },
}));
vi.mock('@/services/api/plugins', () => ({ pluginsApi: { getConfig: mocks.config } }));
vi.mock('@/services/api/bpsSettings', () => ({
  bpsSettingsApi: { get: mocks.oldSettings, save: mocks.oldSave },
}));
vi.mock('@/services/api/bpsModels', async (original) => ({
  ...(await original<object>()),
  bpsModelsApi: { get: mocks.oldModels, save: mocks.oldModelsSave },
}));
vi.mock('@/services/api/bpsAccess', async (original) => ({
  ...(await original<object>()),
  bpsAccessApi: { query: mocks.access },
}));

let renderer: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  mocks.auth = { apiBase: 'https://panel.invalid', managementKey: 'TEST_ONLY' };
  mocks.capabilities.mockResolvedValue({ bps_login_mode: 'plugin' });
  mocks.list.mockResolvedValue({
    files: [{ name: 'plugin.json', id: 'plugin-id', type: 'cpa-bps', auth_index: 'plugin-index' }],
  });
  mocks.models.mockResolvedValue([{ id: 'gpt-test' }]);
  mocks.config.mockResolvedValue({ enabled: true, auto_discover: true, models: [] });
  mocks.oldModels.mockResolvedValue({ models: [], revision: 'v1', connection_ref: 'test' });
  mocks.oldSettings.mockResolvedValue({
    enabled: true,
    images_enabled: true,
    image_transport: 'native',
    image_relay_base_url: '',
    limits: {},
    connection_ref: 'test',
  });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});

describe('系统设置的BPS通道路由', () => {
  it('插件模式显示已导入cpa-bps凭证和注册目录，不调用旧BPS接口', async () => {
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    const text = JSON.stringify(renderer!.toJSON());
    expect(text).toContain('plugin.json');
    expect(text).toContain('gpt-test');
    expect(text).not.toContain('暂无BPS凭证');
    expect(text).not.toContain('保存 BPS 模型清单');
    expect(mocks.models).toHaveBeenCalledWith('plugin-id', mocks.auth);
    expect(mocks.oldModels).not.toHaveBeenCalled();
    expect(mocks.oldSettings).not.toHaveBeenCalled();
    expect(mocks.oldModelsSave).not.toHaveBeenCalled();
    expect(mocks.access).not.toHaveBeenCalled();
  });
  it('自动发现空清单不是清空模型；手动配置只展示，不覆盖插件配置', async () => {
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('模型发布方式：","自动发现并发布');
    expect(renderer!.root.findAllByType('textarea')).toHaveLength(0);
    expect(mocks.config).toHaveBeenCalledWith('cpa-bps', mocks.auth);
    mocks.config.mockResolvedValue({ auto_discover: true, models: ['manual-model'] });
    await act(async () => {
      renderer!.root.findByType('button').props.onClick();
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('手动列表（1 个）');
    expect(mocks.oldSave).not.toHaveBeenCalled();
  });
  it('混合凭证时新区域仅列插件；内置模式保留旧版两卡', async () => {
    mocks.list.mockResolvedValue({
      files: [
        { name: 'plugin.json', id: 'plugin-id', type: 'cpa-bps' },
        { name: 'legacy.json', type: 'bps', auth_index: 'old' },
      ],
    });
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('plugin.json');
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('legacy.json');
    mocks.capabilities.mockResolvedValue({ bps_login_mode: 'builtin' });
    mocks.auth = { ...mocks.auth, apiBase: 'https://builtin.invalid' };
    await act(async () => {
      renderer!.update(<BPSSettingsCard />);
    });
    const text = JSON.stringify(renderer!.toJSON());
    expect(text).toContain('旧版 BPS 模型设置');
    expect(text).toContain('旧版 BPS 协议与图片输入');
    expect(text).toContain('legacy.json');
    expect(text).not.toContain('plugin.json');
  });
  it('能力查询失败不暴露旧配置写入口，重试后读取插件', async () => {
    mocks.capabilities.mockRejectedValueOnce(new Error('offline'));
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('BPS 通道未确认');
    expect(mocks.oldSettings).not.toHaveBeenCalled();
    expect(mocks.oldModels).not.toHaveBeenCalled();
    await act(async () => {
      renderer!.root.findByType('button').props.onClick();
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('plugin.json');
  });
  it('空凭证与注册目录读取失败分别显示，不把未知写成零', async () => {
    mocks.models.mockRejectedValueOnce(new Error('offline'));
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('模型状态未知');
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('gpt-test');
    mocks.list.mockResolvedValue({ files: [] });
    await act(async () => {
      renderer!.root.findByType('button').props.onClick();
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('暂无 cpa-bps 插件凭证');
  });
  it('切换凭证丢弃旧目录，切换连接后旧响应不回写', async () => {
    let completeA!: (value: { id: string }[]) => void;
    mocks.list.mockResolvedValue({
      files: [
        { name: 'a.json', id: 'a', type: 'cpa-bps' },
        { name: 'b.json', id: 'b', type: 'cpa-bps' },
      ],
    });
    mocks.models.mockImplementation((id) =>
      id === 'a'
        ? new Promise((resolve) => {
            completeA = resolve;
          })
        : Promise.resolve([{ id: 'model-b' }])
    );
    await act(async () => {
      renderer = create(<BPSSettingsCard />);
    });
    const second = renderer!.root.findAllByType('option')[1].props.value;
    await act(async () => {
      renderer!.root.findByType('select').props.onChange({ target: { value: second } });
    });
    await act(async () => {
      completeA([{ id: 'late-a' }]);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('model-b');
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('late-a');
    let completeB!: (value: { id: string }[]) => void;
    mocks.models.mockImplementation(
      () =>
        new Promise((resolve) => {
          completeB = resolve;
        })
    );
    await act(async () => {
      renderer!.root.findByType('button').props.onClick();
    });
    mocks.auth = { apiBase: 'https://other.invalid', managementKey: 'OTHER_TEST' };
    mocks.list.mockResolvedValue({ files: [{ name: 'other.json', id: 'other', type: 'cpa-bps' }] });
    mocks.models.mockResolvedValue([{ id: 'other-model' }]);
    await act(async () => {
      renderer!.update(<BPSSettingsCard />);
    });
    await act(async () => {
      completeB([{ id: 'late-b' }]);
    });
    expect(JSON.stringify(renderer!.toJSON())).toContain('other-model');
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('late-b');
    expect(mocks.config).toHaveBeenLastCalledWith('cpa-bps', mocks.auth);
  });
});
