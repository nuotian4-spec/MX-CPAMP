import { Button } from '@/components/ui/Button';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BPSModelsPanel, BPSAccountModelsSummary } from './BPSModelsPanel';
import { bpsAccessApi, type BPSAccess } from '@/services/api/bpsAccess';
import { bpsModelsApi } from '@/services/api/bpsModels';
import { bpsSettingsApi } from '@/services/api/bpsSettings';
import { authFilesApi } from '@/services/api/authFiles';
vi.mock('@/services/api/bpsModels', async (original) => ({
  ...(await original<object>()),
  bpsModelsApi: { get: vi.fn(), save: vi.fn() },
}));
vi.mock('@/services/api/bpsAccess', async (original) => ({
  ...(await original<object>()),
  bpsAccessApi: { query: vi.fn() },
}));
vi.mock('@/services/api/bpsSettings', () => ({ bpsSettingsApi: { get: vi.fn() } }));
vi.mock('@/services/api/authFiles', () => ({ authFilesApi: { list: vi.fn() } }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const scope = { apiBase: 'http://panel.invalid', managementKey: 'TEST' };
const receipt = (index: string, models: string[]): BPSAccess => ({
  provider: 'bps',
  auth_index: index,
  auth_id: index,
  queried_at: '2026-11-11T00:00:00Z',
  allowed: true,
  models,
  upstream_status: 200,
  error: null,
  quota: null,
  inference_verified: false,
  auto_recover_eligible: false,
});
let renderer: ReactTestRenderer;
const click = async (label: string) => {
  await act(async () => {
    const button = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === label);
    if (!button) throw new Error(label);
    await button.props.onClick();
  });
};
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(bpsSettingsApi.get).mockResolvedValue({
    enabled: true,
    images_enabled: true,
    image_transport: 'native',
    image_relay_base_url: '',
    limits: {},
    connection_ref: 'reference',
  });
  vi.mocked(bpsModelsApi.get).mockResolvedValue({
    models: ['gpt-old'],
    connection_ref: 'reference',
    revision: 'v1',
  });
  vi.mocked(authFilesApi.list).mockResolvedValue({
    files: [
      { name: 'one.json', type: 'bps', auth_index: 'one' },
      { name: 'two.json', type: 'bps', auth_index: 'two' },
    ],
  });
  vi.mocked(bpsAccessApi.query).mockImplementation(async (index) =>
    receipt(index, index === 'one' ? ['gpt-one'] : ['gpt-two'])
  );
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
});
describe('BPS模型发现与人工发布', () => {
  it('发现和一键选用不写配置，保存/清空才提交全局清单', async () => {
    await act(async () => {
      renderer = create(<BPSModelsPanel scope={scope} />);
    });
    await click('获取官方允许模型');
    await click('一键选用到草稿');
    expect(renderer.root.findByType('textarea').props.value).toBe('gpt-one');
    expect(bpsModelsApi.save).not.toHaveBeenCalled();
    await click('保存 BPS 模型清单');
    expect(bpsModelsApi.save).toHaveBeenCalledWith(
      expect.objectContaining({ revision: 'v1' }),
      ['gpt-one'],
      scope
    );
    await act(async () =>
      renderer.root.findByType('textarea').props.onChange({ target: { value: '' } })
    );
    await click('保存 BPS 模型清单');
    expect(bpsModelsApi.save).toHaveBeenLastCalledWith(expect.anything(), [], scope);
  });
  it('两份凭证发现独立，查询失败保留草稿，连接切换清除旧观察', async () => {
    await act(async () => {
      renderer = create(<BPSModelsPanel scope={scope} />);
    });
    await click('获取官方允许模型');
    await act(async () =>
      renderer.root.findByType('select').props.onChange({ target: { value: 'two' } })
    );
    expect(JSON.stringify(renderer.toJSON())).not.toContain('gpt-one');
    await click('获取官方允许模型');
    expect(JSON.stringify(renderer.toJSON())).toContain('gpt-two');
    vi.mocked(bpsAccessApi.query).mockRejectedValueOnce(new Error('failed'));
    await click('获取官方允许模型');
    expect(renderer.root.findByType('textarea').props.value).toBe('gpt-old');
    await act(async () =>
      renderer.update(<BPSModelsPanel scope={{ ...scope, apiBase: 'http://other.invalid' }} />)
    );
    expect(JSON.stringify(renderer.toJSON())).not.toContain('gpt-two');
  });
  it('账号发布/注册与官方权限独立，未查询未确认且不匹配不宣称可调用', async () => {
    await act(async () => {
      renderer = create(
        <BPSAccountModelsSummary
          scope={scope}
          authIndex="one"
          connectionRef="reference"
          registeredModels={['gpt-old']}
        />
      );
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('权限未确认');
    await click('查询本凭证官方权限');
    expect(JSON.stringify(renderer.toJSON())).toContain('权限不匹配');
    await act(async () =>
      renderer.update(
        <BPSAccountModelsSummary
          scope={scope}
          authIndex="two"
          connectionRef="reference"
          registeredModels={['gpt-old']}
        />
      )
    );
    expect(JSON.stringify(renderer.toJSON())).toContain('权限未确认');
    expect(bpsModelsApi.save).not.toHaveBeenCalled();
  });
  it('展示凭证后实例变化仍发送旧引用，保存后读取失败锁定写入直到局部刷新', async () => {
    await act(async () => {
      renderer = create(<BPSModelsPanel scope={scope} />);
    });
    vi.mocked(bpsSettingsApi.get).mockResolvedValue({
      enabled: true,
      images_enabled: true,
      image_transport: 'native',
      image_relay_base_url: '',
      limits: {},
      connection_ref: 'other-instance',
    });
    await click('获取官方允许模型');
    expect(bpsAccessApi.query).toHaveBeenLastCalledWith('one', scope, 'reference');
    await act(async () =>
      renderer.root.findByType('textarea').props.onChange({ target: { value: 'gpt-draft' } })
    );
    vi.mocked(bpsModelsApi.get).mockRejectedValueOnce(new Error('read failed'));
    await click('保存 BPS 模型清单');
    expect(renderer.root.findByType('textarea').props.value).toBe('gpt-draft');
    expect(
      renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '保存 BPS 模型清单')?.props.disabled
    ).toBe(true);
    vi.mocked(bpsSettingsApi.get).mockResolvedValue({
      enabled: true,
      images_enabled: true,
      image_transport: 'native',
      image_relay_base_url: '',
      limits: {},
      connection_ref: 'reference',
    });
    await click('局部刷新发布清单');
    expect(renderer.root.findByType('textarea').props.value).toBe('gpt-draft');
    expect(
      renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '保存 BPS 模型清单')?.props.disabled
    ).toBe(false);
  });
});
