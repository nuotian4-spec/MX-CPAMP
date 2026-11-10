import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { InternalLoginCards } from './InternalLoginCards';
import { Button } from '@/components/ui/Button';
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  get: vi.fn(),
  active: vi.fn(),
  cancel: vi.fn(),
  startBPS: vi.fn(),
  status: vi.fn(),
  callback: vi.fn(),
  cancelOAuth: vi.fn(),
  publish: vi.fn(),
}));
vi.mock('@/services/api/loginBatches', async (original) => ({
  ...(await original<typeof import('@/services/api/loginBatches')>()),
  loginBatchesApi: mocks,
}));
vi.mock('@/services/api/loginTasks', async (original) => ({
  ...(await original<typeof import('@/services/api/loginTasks')>()),
  loginTasksApi: mocks,
}));
vi.mock('@/stores', () => ({ publishAccountCredentialMutationRevision: mocks.publish }));
vi.mock('@/features/monitoring/codexInspection', () => ({
  createCodexInspectionConnectionFingerprint: () => 'connection',
}));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'admin' };
const running = {
  id: 'batch',
  profile: 'codex',
  status: 'running',
  items: [
    {
      id: 'task',
      profile: 'codex',
      status: 'running',
      line_no: 1,
      email_hint: 'f***@example.invalid',
    },
  ],
};
describe('批量与BPS授权入口', () => {
  let renderer: ReactTestRenderer;
  let textareas: Array<{ value: string }>;
  let callbacks: Array<{ value: string }>;
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    textareas = [];
    callbacks = [];
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('window', { setInterval, clearInterval });
    vi.stubGlobal('crypto', { randomUUID: () => '11111111-1111-4111-8111-111111111111' });
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    mocks.active.mockResolvedValue(null);
    mocks.cancelOAuth.mockResolvedValue({ status: 'ok' });
    mocks.get.mockResolvedValue(running);
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(<InternalLoginCards scope={scope} />, {
        createNodeMock: (element) => {
          if (element.type === 'textarea') {
            const node = { value: '' };
            textareas.push(node);
            return node;
          }
          if (element.type === 'input' && element.props.type === 'password') {
            const node = { value: '' };
            callbacks.push(node);
            return node;
          }
          return null;
        },
      });
    });
  }
  async function submit() {
    textareas[0].value = 'fake@example.invalid----PASSWORD_CANARY----ABCDEFGHIJKLMNOP';
    await act(async () => {
      await renderer.root.findAllByType('form')[0].props.onSubmit({ preventDefault() {} });
    });
  }
  it('批量表单只保存掩码预览，提交后清空材料', async () => {
    mocks.create.mockResolvedValue(running);
    await render();
    await submit();
    expect(textareas[0].value).toBe('');
    expect(mocks.create.mock.calls[0][0].items[0].password).toBe('');
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('Codex 2FA 批量导入');
    expect(text).toContain('BPS 2FA 批量导入');
    expect(text).not.toContain('PASSWORD_CANARY');
    expect(text).not.toContain('ABCDEFGHIJKLMNOP');
  });
  it('额外验证保留实际阶段且不发成功事件', async () => {
    mocks.create.mockResolvedValue(running);
    mocks.get.mockResolvedValue({
      ...running,
      status: 'completed',
      items: [
        {
          ...running.items[0],
          status: 'needs_user_action',
          stage: 'password',
          reason: 'security_check',
        },
      ],
    });
    await render();
    await submit();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('官方安全检查');
    expect(mocks.publish).not.toHaveBeenCalled();
  });
  it('离页与切实例不取消批次，重新进入查询活动批次', async () => {
    mocks.active.mockResolvedValue(running);
    await render();
    expect(JSON.stringify(renderer.toJSON())).toContain('正在逐条登录');
    await act(async () => {
      renderer.update(
        <InternalLoginCards scope={{ apiBase: 'https://other.invalid', managementKey: 'other' }} />
      );
    });
    expect(mocks.cancel).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it('创建响应丢失按已知编号查询，不自动重交；404后停止', async () => {
    mocks.create.mockRejectedValue(new Error('lost'));
    mocks.get.mockRejectedValue({ status: 404 });
    await render();
    await submit();
    expect(JSON.stringify(renderer.toJSON())).toContain('回执未确认');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(mocks.get).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111', scope);
    expect(JSON.stringify(renderer.toJSON())).toContain('batch_not_found');
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.cancel).not.toHaveBeenCalled();
  });
  it('取消剩余条目并等待当前真实结果', async () => {
    mocks.create.mockResolvedValue(running);
    mocks.cancel.mockResolvedValue({ ...running, status: 'cancelling' });
    await render();
    await submit();
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '取消剩余条目')
        ?.props.onClick();
    });
    expect(mocks.cancel).toHaveBeenCalledWith('batch', scope);
    expect(JSON.stringify(renderer.toJSON())).toContain('等待当前条目收敛');
  });
  it('BPS生成可打开和复制的授权链接，回调ACK不算保存成功', async () => {
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/authorize', state: 'state' });
    mocks.callback.mockResolvedValue({ status: 'ok' });
    mocks.status.mockResolvedValue({ status: 'wait' });
    await render();
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '生成授权链接')
        ?.props.onClick();
    });
    expect(mocks.startBPS).toHaveBeenCalledWith(scope);
    expect(renderer.root.findByType('a').props.href).toBe('https://auth.invalid/authorize');
    expect(renderer.root.findByType('a').props.rel).toContain('noreferrer');
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '复制授权链接')
        ?.props.onClick();
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://auth.invalid/authorize');
    callbacks[0].value = 'https://callback.invalid/?code=CODE&state=state';
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '提交回调')
        ?.props.onClick();
    });
    expect(callbacks[0].value).toBe('');
    expect(mocks.publish).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).toContain('等待 CPA 保存注册结果');
    mocks.status.mockResolvedValue({ status: 'ok' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
});
