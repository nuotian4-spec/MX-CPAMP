import { Profiler } from 'react';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { InternalLoginCards } from './InternalLoginCards';
import { Button } from '@/components/ui/Button';
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  get: vi.fn(),
  active: vi.fn(),
  latest: vi.fn(),
  cancel: vi.fn(),
  startBPS: vi.fn(),
  status: vi.fn(),
  callback: vi.fn(),
  cancelOAuth: vi.fn(),
  capabilities: vi.fn(),
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
    mocks.latest.mockResolvedValue(null);
    mocks.cancelOAuth.mockResolvedValue({ status: 'ok' });
    mocks.get.mockResolvedValue(running);
    mocks.capabilities.mockResolvedValue({ password_login_configured: true });
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
          if (
            element.type === 'input' &&
            element.props &&
            typeof element.props === 'object' &&
            'type' in element.props &&
            element.props.type === 'password'
          ) {
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
  const createButtons = () =>
    renderer.root.findAllByType(Button).filter((node) => node.props.children === '开始批量登录');
  it('未配置登录器时禁用两处2FA，但手工BPS授权独立可用', async () => {
    mocks.capabilities.mockResolvedValue({ password_login_configured: false });
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/authorize', state: 'state' });
    await render();
    expect(mocks.capabilities).toHaveBeenCalledWith(scope);
    expect(createButtons()).toHaveLength(2);
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    expect(renderer.root.findAllByType('textarea').every((node) => node.props.disabled)).toBe(true);
    expect(JSON.stringify(renderer.toJSON())).toContain('当前服务未配置2FA登录器');
    await submit();
    expect(mocks.create).not.toHaveBeenCalled();
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '生成授权链接')
        ?.props.onClick();
    });
    expect(mocks.startBPS).toHaveBeenCalledWith(scope);
  });
  it.each(['error', 'missing', 'nonboolean', 'null'] as const)(
    '配置%s时不开放2FA，也不读取输入',
    async (kind) => {
      if (kind === 'error') mocks.capabilities.mockRejectedValue(new Error('offline'));
      else
        mocks.capabilities.mockResolvedValue(
          kind === 'missing' ? {} : kind === 'null' ? null : { password_login_configured: 'true' }
        );
      await render();
      expect(createButtons().every((button) => button.props.disabled)).toBe(true);
      expect(JSON.stringify(renderer.toJSON())).toContain('2FA登录器配置状态未确认');
      await submit();
      expect(mocks.create).not.toHaveBeenCalled();
      expect(textareas[0].value).toContain('PASSWORD_CANARY');
      expect(renderer.root.findAllByType('details')).toHaveLength(0);
    }
  );
  it('配置加载期间禁止回车提交，确认配置后才开放', async () => {
    let resolve!: (value: { password_login_configured: boolean }) => void;
    mocks.capabilities.mockReturnValue(new Promise((done) => (resolve = done)));
    await render();
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    expect(JSON.stringify(renderer.toJSON())).toContain('正在确认2FA登录器配置');
    await submit();
    expect(mocks.create).not.toHaveBeenCalled();
    await act(async () => resolve({ password_login_configured: true }));
    expect(createButtons().every((button) => !button.props.disabled)).toBe(true);
  });
  it('切实例当帧禁用旧配置，返回原实例也重新等待配置', async () => {
    await render();
    expect(createButtons().every((button) => !button.props.disabled)).toBe(true);
    const disabledSnapshots: boolean[] = [];
    mocks.capabilities.mockReturnValue(new Promise(() => {}));
    const nextScope = { apiBase: 'https://other.invalid', managementKey: 'other' };
    const view = (currentScope: typeof scope) => (
      <Profiler
        id="login"
        onRender={() => {
          disabledSnapshots.push(createButtons().every((button) => button.props.disabled));
        }}
      >
        <InternalLoginCards scope={currentScope} />
      </Profiler>
    );
    await act(async () => renderer.update(view(scope)));
    disabledSnapshots.length = 0;
    await act(async () => renderer.update(view(nextScope)));
    expect(disabledSnapshots.length).toBeGreaterThan(0);
    expect(disabledSnapshots.every(Boolean)).toBe(true);
    await act(async () => renderer.update(view(scope)));
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    await submit();
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it('旧实例迟到的配置响应不解锁当前实例', async () => {
    let resolveOld!: (value: { password_login_configured: boolean }) => void;
    mocks.capabilities.mockReturnValueOnce(new Promise((done) => (resolveOld = done)));
    await render();
    mocks.capabilities.mockReturnValue(new Promise(() => {}));
    await act(async () =>
      renderer.update(
        <InternalLoginCards scope={{ apiBase: 'https://other.invalid', managementKey: 'other' }} />
      )
    );
    await act(async () => resolveOld({ password_login_configured: true }));
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    expect(JSON.stringify(renderer.toJSON())).toContain('正在确认2FA登录器配置');
  });
  it('未配置登录器不阻断已有批次查询和取消', async () => {
    mocks.capabilities.mockResolvedValue({ password_login_configured: false });
    mocks.active.mockResolvedValue(running);
    mocks.cancel.mockResolvedValue({ ...running, status: 'cancelling' });
    await render();
    await act(async () => vi.advanceTimersByTimeAsync(1600));
    expect(mocks.get).toHaveBeenCalledWith('batch', scope);
    const cancel = renderer.root
      .findAllByType(Button)
      .find((node) => node.props.children === '取消剩余条目');
    expect(cancel?.props.disabled).toBe(false);
    await act(async () => cancel?.props.onClick());
    expect(mocks.cancel).toHaveBeenCalledWith('batch', scope);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it.each([400, 502])('回调HTTP %s显示明确拒绝或结果待确认', async (status) => {
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/authorize', state: 'state' });
    mocks.callback.mockRejectedValue({ status, message: 'RAW_TOKEN_CANARY' });
    await render();
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '生成授权链接')
        ?.props.onClick();
    });
    callbacks[0].value = 'https://callback.invalid/?code=CODE_CANARY&state=state';
    await act(async () => {
      await renderer.root
        .findAllByType(Button)
        .find((node) => node.props.children === '提交回调')
        ?.props.onClick();
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain(status === 400 ? '回调地址未通过校验' : '回调结果未确认');
    expect(text).not.toContain('RAW_TOKEN_CANARY');
    expect(text).not.toContain('CODE_CANARY');
    expect(callbacks[0].value).toBe('');
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.publish).not.toHaveBeenCalled();
  });
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
  it.each(['completed', 'interrupted'] as const)(
    '离页期间%s后重新进入恢复终态摘要',
    async (status) => {
      mocks.active.mockResolvedValue(running);
      await render();
      await act(async () => renderer.unmount());
      mocks.active.mockResolvedValue(null);
      mocks.latest.mockImplementation(async (profile: string) =>
        profile === 'codex'
          ? {
              ...running,
              status,
              items: [{ ...running.items[0], status: 'succeeded', auth_index: '0123456789abcdef' }],
            }
          : null
      );
      await render();
      expect(mocks.latest).toHaveBeenCalledWith('codex', scope);
      expect(JSON.stringify(renderer.toJSON())).toContain(
        status === 'completed' ? '批次已完成' : '批次已中断'
      );
      expect(JSON.stringify(renderer.toJSON())).toContain('0123456789abcdef');
      expect(mocks.create).not.toHaveBeenCalled();
      expect(mocks.cancel).not.toHaveBeenCalled();
    }
  );
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
  it('成功摘要只显示既有凭证索引，不显示带邮箱的文件名', async () => {
    mocks.create.mockResolvedValue(running);
    mocks.get.mockResolvedValue({
      ...running,
      status: 'completed',
      items: [
        {
          ...running.items[0],
          status: 'succeeded',
          auth_index: '0123456789abcdef',
          auth_id: 'codex-user@example.invalid-plan.json',
        },
      ],
    });
    await render();
    await submit();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('0123456789abcdef');
    expect(text).not.toContain('user@example.invalid');
    expect(text).not.toContain('plan.json');
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
  it('已保存但索引未知明确提示且不自动重放', async () => {
    mocks.create.mockResolvedValue(running);
    mocks.get.mockResolvedValue({
      ...running,
      status: 'interrupted',
      error_code: 'credential_mapping_unconfirmed',
      items: [
        { ...running.items[0], status: 'failed', error_code: 'credential_mapping_unconfirmed' },
      ],
    });
    await render();
    await submit();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('已保存，凭证索引待核实');
    expect(text).not.toContain('登录未完成');
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.publish).toHaveBeenCalledTimes(1);
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
