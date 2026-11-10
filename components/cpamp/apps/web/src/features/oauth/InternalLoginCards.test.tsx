import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { InternalLoginCards } from './InternalLoginCards';
import { Button } from '@/components/ui/Button';
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  get: vi.fn(),
  cancel: vi.fn(),
  startBPS: vi.fn(),
  status: vi.fn(),
  callback: vi.fn(),
  cancelOAuth: vi.fn(),
  publish: vi.fn(),
}));
vi.mock('@/services/api/loginTasks', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/api/loginTasks')>()),
  loginTasksApi: mocks,
}));
vi.mock('@/stores', () => ({ publishAccountCredentialMutationRevision: mocks.publish }));
vi.mock('@/features/monitoring/codexInspection', () => ({
  createCodexInspectionConnectionFingerprint: () => 'connection',
}));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'admin' };
describe('四入口新增卡片', () => {
  let renderer: ReactTestRenderer;
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.cancelOAuth.mockResolvedValue({status:'ok'});
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('window', { setInterval, clearInterval });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  it('显示三个新增入口且密码种子为一次性掩码输入', async () => {
    await act(async () => {
      renderer = create(<InternalLoginCards scope={scope} />);
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('BPS OAuth');
    expect(text).toContain('BPS 2FA');
    expect(text).toContain('Codex 2FA');
    expect(text).toContain('真实可兑换性待用户授权验收');
    const inputs = renderer.root
      .findAllByType('input')
      .filter((node) => ['password', 'totp_secret'].includes(node.props.name));
    expect(inputs).toHaveLength(4);
    inputs.forEach((node) => {
      expect(node.props.type).toBe('password');
      expect(node.props.value).toBeUndefined();
    });
  });
  it('额外验证如实显示且不发布成功事件', async () => {
    mocks.create.mockResolvedValue({ id: 'task', profile: 'codex', status: 'running' });
    mocks.get.mockResolvedValue({
      id: 'task',
      profile: 'codex',
      status: 'needs_user_action',
      error_code: 'needs_user_action',
    });
    const values: Record<string, string> = {
      email: 'fake@example.invalid',
      password: 'PASSWORD_CANARY',
      totp_secret: 'TOTP_CANARY',
    };
    vi.stubGlobal(
      'FormData',
      class {
        get(name: string) {
          return values[name];
        }
        delete(name: string) {
          delete values[name];
        }
      }
    );
    await act(async () => {
      renderer = create(<InternalLoginCards scope={scope} />);
    });
    await act(async () => {
      await renderer.root
        .findAllByType('form')[0]
        .props.onSubmit({ preventDefault() {}, currentTarget: {} });
    });
    expect(mocks.create).toHaveBeenCalledWith(
      { profile: 'codex', email: 'fake@example.invalid', password: '', totp_secret: '' },
      scope
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1100);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('需要在官方页面完成额外验证');
    expect(mocks.publish).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).not.toContain('PASSWORD_CANARY');
  });
  it('离页与切实例取消旧任务，正常轮询不取消', async () => {
    mocks.create.mockResolvedValue({ id: 'old-task', profile: 'codex', status: 'running' });
    mocks.get.mockResolvedValue({ id: 'old-task', profile: 'codex', status: 'running' });
    mocks.cancel.mockResolvedValue({ id: 'old-task', profile: 'codex', status: 'cancelling' });
    vi.stubGlobal(
      'FormData',
      class {
        get(name: string) {
          return name === 'email' ? 'fake@example.invalid' : 'CANARY';
        }
        delete() {}
      }
    );
    await act(async () => {
      renderer = create(<InternalLoginCards scope={scope} />);
    });
    await act(async () => {
      await renderer.root
        .findAllByType('form')[0]
        .props.onSubmit({ preventDefault() {}, currentTarget: {} });
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1100);
    });
    expect(mocks.cancel).not.toHaveBeenCalled();
    await act(async () => {
      renderer.update(
        <InternalLoginCards scope={{ apiBase: 'https://other.invalid', managementKey: 'other' }} />
      );
    });
    expect(mocks.cancel).toHaveBeenCalledWith('old-task', scope);
  });
  it('BPS OAuth仅创建固定BPS授权', async () => {
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/authorize', state: 'state' });
    await act(async () => {
      renderer = create(<InternalLoginCards scope={scope} />);
    });
    const button = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === '打开授权入口');
    await act(async () => {
      await button?.props.onClick();
    });
    expect(mocks.startBPS).toHaveBeenCalledWith(scope);
    expect(renderer.root.findByType('a').props.rel).toContain('noreferrer');
  });
});
