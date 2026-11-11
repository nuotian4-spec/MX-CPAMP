import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BPSSettingsPanel } from './BPSSettingsCard';
const mocks = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn() }));
vi.mock('@/services/api/bpsSettings', () => ({ bpsSettingsApi: mocks }));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'MANAGER_ADMIN' };
const settings = { enabled: true, images_enabled: true, image_transport: 'native', image_relay_base_url: '', connection_ref: 'a'.repeat(32), limits: { ttl_seconds: 1800 } };
describe('BPS协议与图片设置', () => {
  let renderer: ReactTestRenderer;
  beforeEach(() => { vi.clearAllMocks(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); mocks.get.mockResolvedValue(settings); mocks.save.mockResolvedValue({ status: 'saved' }); });
  afterEach(async () => { if (renderer) await act(async () => renderer.unmount()); vi.unstubAllGlobals(); });
  const render = async () => { await act(async () => { renderer = create(<BPSSettingsPanel scope={scope} />); }); };
  const submit = async () => { await act(async () => { await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} }); }); };
  it('默认原生，关闭图片明确告知历史和工具截图被拒绝', async () => {
    await render();
    expect(renderer.root.findByType('select').props.value).toBe('native');
    expect(JSON.stringify(renderer.toJSON())).toContain('含对话、工具截图或历史图片');
    await act(async () => { renderer.root.findAllByType('input').filter((input) => input.props.type === 'checkbox')[1].props.onChange({ target: { checked: false } }); });
    await submit();
    expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, images_enabled: false, connection_ref: settings.connection_ref }), scope);
    expect(mocks.save.mock.calls[0][0]).not.toHaveProperty('limits');
  });
  it('中转模式没有HTTPS基址时零写入', async () => {
    await render();
    await act(async () => { renderer.root.findByType('select').props.onChange({ target: { value: 'relay' } }); });
    await submit();
    expect(mocks.save).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).toContain('有效的HTTPS基址');
  });
  it('切换实例时重新装载引用并保留每次请求的作用域', async () => {
    await render();
    const other = { apiBase: 'https://other.invalid', managementKey: 'OTHER_ADMIN' };
    mocks.get.mockResolvedValue({ ...settings, connection_ref: 'b'.repeat(32) });
    await act(async () => { renderer.update(<BPSSettingsPanel key="other" scope={other} />); });
    await submit();
    expect(mocks.save).toHaveBeenLastCalledWith(expect.objectContaining({ connection_ref: 'b'.repeat(32) }), other);
  });
});
