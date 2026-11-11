import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CredentialConcurrencyPanel } from './CredentialConcurrencyPage';
const mocks = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn() }));
vi.mock('@/services/api/credentialConcurrency', () => ({ credentialConcurrencyApi: mocks }));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'admin' };
const credential = {
  auth_id: 'codex.json',
  auth_index: 'INDEX',
  name: '账号一',
  provider: 'codex',
  source: 'file',
  editable: true,
  read_only_reason: '',
  concurrency_limit: null,
  effective_limit: 20,
  limit_source: 'default',
  active: 3,
  pinned_waiting: 2,
};
const snapshot = {
  mode: 'local',
  connection_ref: 'a'.repeat(32),
  default_limit: 20,
  pool_waiting: 7,
  credentials: [credential],
};
describe('凭证并发页面', () => {
  let renderer: ReactTestRenderer;
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('window', { setInterval, clearInterval });
    mocks.list.mockResolvedValue(snapshot);
    mocks.update.mockResolvedValue({ status: 'saved' });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(<CredentialConcurrencyPanel scope={scope} />);
    });
  }
  it('公共等待独立展示，每份凭证显示占用、有效上限及定向等待', async () => {
    await render();
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('公共池等待');
    expect(text).toContain('定向等待');
    expect(text).toContain('统一默认');
    expect(text).toContain('同一凭证的全部模型共用额度');
    expect(mocks.list).toHaveBeenCalledWith(scope);
  });
  it('只提交凭证编号与整数上限，留空恢复默认', async () => {
    await render();
    await act(async () => {
      renderer.root.findByType('input').props.onChange({ target: { value: '8' } });
    });
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update).toHaveBeenLastCalledWith('codex.json', 8, snapshot.connection_ref, scope);
    await act(async () => {
      renderer.root.findByType('input').props.onChange({ target: { value: '' } });
    });
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update).toHaveBeenLastCalledWith(
      'codex.json',
      null,
      snapshot.connection_ref,
      scope
    );
  });
  it('拒绝小数和零，配置冲突如实显示', async () => {
    await render();
    await act(async () => {
      renderer.root.findByType('input').props.onChange({ target: { value: '1.5' } });
    });
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update).not.toHaveBeenCalled();
    mocks.update.mockRejectedValue(new Error('配置已被其他操作更新，请刷新后重试'));
    await act(async () => {
      renderer.root.findByType('input').props.onChange({ target: { value: '8' } });
    });
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('配置已被其他操作更新');
  });
  it('轮询切换实例后重建同名凭证表单，提交绑定新快照引用', async () => {
    await render();
    await act(async () => {
      renderer.root.findByType('input').props.onChange({ target: { value: '8' } });
    });
    mocks.list.mockResolvedValue({ ...snapshot, connection_ref: 'b'.repeat(32) });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100);
    });
    expect(renderer.root.findByType('input').props.value).toBe('');
    await act(async () => {
      await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update).toHaveBeenLastCalledWith('codex.json', null, 'b'.repeat(32), scope);
  });
  it('Home不显示本地计数或编辑；运行时和keyless只读', async () => {
    mocks.list.mockResolvedValue({ mode: 'home', credentials: [] });
    await render();
    expect(renderer.root.findAllByType('form')).toHaveLength(0);
    expect(JSON.stringify(renderer.toJSON())).not.toContain('公共池等待');
    mocks.list.mockResolvedValue({
      ...snapshot,
      credentials: [
        { ...credential, source: 'runtime', editable: false, read_only_reason: 'runtime_source' },
        {
          ...credential,
          auth_id: 'keyless',
          source: 'config',
          editable: false,
          read_only_reason: 'keyless_group',
        },
      ],
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100);
    });
    expect(renderer.root.findAllByType('form')).toHaveLength(0);
    expect(JSON.stringify(renderer.toJSON())).toContain('无独立Key的兼容组');
  });
});
