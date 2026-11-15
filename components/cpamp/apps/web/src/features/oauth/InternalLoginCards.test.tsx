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
    vi.stubGlobal('window', { setInterval, clearInterval, setTimeout, clearTimeout });
    vi.stubGlobal('crypto', { randomUUID: () => '11111111-1111-4111-8111-111111111111' });
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    mocks.active.mockResolvedValue(null);
    mocks.latest.mockResolvedValue(null);
    mocks.cancelOAuth.mockResolvedValue({ status: 'ok', cancelled: true });
    mocks.get.mockResolvedValue(running);
    mocks.capabilities.mockResolvedValue({
      password_login_configured: true,
      bps_login_mode: 'builtin',
    });
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
  it.each(['plugin', 'unavailable', undefined, 'unknown'])(
    'BPS模式%s不开放旧登录，Codex保留',
    async (mode) => {
      mocks.capabilities.mockResolvedValue({
        password_login_configured: true,
        bps_login_mode: mode,
      });
      await render();
      expect(renderer.root.findAllByType('textarea')).toHaveLength(1);
      expect(createButtons()).toHaveLength(1);
      expect(createButtons()[0].props.disabled).toBe(false);
      expect(
        renderer.root.findAllByType(Button).some((node) => node.props.children === '生成授权链接')
      ).toBe(false);
      expect(mocks.startBPS).not.toHaveBeenCalled();
      expect(mocks.active.mock.calls.every((call) => call[0] !== 'bps')).toBe(true);
      expect(JSON.stringify(renderer.toJSON())).toContain(
        mode === 'plugin' ? 'BPS 2FA 暂未接入' : 'BPS 登录模式未确认'
      );
    }
  );
  it.each([
    ['session_missing_or_expired', false, '尚未提交回调', true],
    ['session_missing_or_expired', true, '保存结果待核实', true],
    ['oauth_exchange_failed', true, '令牌兑换失败', false],
    ['oauth_identity_invalid', true, '授权身份校验失败', false],
    ['credential_commit_failed', true, '保存或注册未全部确认', true],
  ] as const)(
    'BPS终态%s保留安全结果并保护未知提交',
    async (error_code, submitted, label, blocked) => {
      mocks.startBPS.mockResolvedValue({
        url: 'https://auth.invalid/URL_CANARY',
        state: 'STATE_CANARY',
      });
      mocks.status.mockResolvedValue({
        status: 'error',
        error_code,
        http_status: 400,
        reason: 'invalid_grant',
        raw: 'SECRET_CANARY',
      });
      mocks.callback.mockResolvedValue({ status: 'ok' });
      await render();
      const button = (label: string) =>
        renderer.root.findAllByType(Button).find((node) => node.props.children === label)!;
      await act(async () => {
        await button('生成授权链接').props.onClick();
      });
      if (submitted) {
        callbacks[0].value = 'https://callback.invalid/?code=CODE_CANARY';
        await act(async () => {
          await button('提交回调').props.onClick();
        });
        expect(button('提交回调').props.disabled).toBe(true);
        expect(callbacks[0].value).toBe('');
      }
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1500);
      });
      const text = JSON.stringify(renderer.toJSON());
      expect(text).toContain(label);
      for (const secret of ['URL_CANARY', 'STATE_CANARY', 'CODE_CANARY', 'SECRET_CANARY'])
        expect(text).not.toContain(secret);
      expect(text).not.toContain('授权失败，请检查官方页面验证');
      expect(button('生成授权链接').props.disabled).toBe(blocked);
      if (blocked)
        await act(async () => {
          await button('生成授权链接').props.onClick();
        });
      expect(mocks.startBPS).toHaveBeenCalledTimes(1);
    }
  );
  it('Codex人工验证入口只定位浏览器授权，保留原批次且不发请求', async () => {
    const scrollIntoView = vi.fn();
    const focus = vi.fn();
    vi.stubGlobal('document', { getElementById: vi.fn(() => ({ scrollIntoView, focus })) });
    mocks.latest.mockImplementation(async (profile: string) => ({
      ...running,
      profile,
      status: 'completed',
      items: [
        { ...running.items[0], profile, status: 'needs_user_action', reason: 'security_check' },
      ],
    }));
    await render();
    const buttons = renderer.root
      .findAllByType(Button)
      .filter((node) => node.props.children === '转到浏览器 OAuth');
    expect(buttons).toHaveLength(1);
    await act(async () => {
      buttons[0].props.onClick();
    });
    expect(document.getElementById).toHaveBeenCalledWith('oauth-provider-codex');
    expect(scrollIntoView).toHaveBeenCalledOnce();
    expect(focus).toHaveBeenCalledOnce();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.startBPS).not.toHaveBeenCalled();
    expect(mocks.publish).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).toContain('新建独立授权');
    expect(JSON.stringify(renderer.toJSON())).toContain('登录已停止，请查看诊断');
  });

  it('批次展示HTTP拒绝类别而不归因官方验证', async () => {
    mocks.latest.mockResolvedValue({
      ...running,
      status: 'completed',
      items: [
        {
          ...running.items[0],
          status: 'needs_user_action',
          stage: 'web_login',
          reason: 'security_check',
          http_status: 403,
          endpoint_category: 'chatgpt_home',
          security_check_subtype: 'html_forbidden_unclassified',
          challenge_header_present: false,
        },
      ],
    });
    await render();
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('具体原因待确认');
    expect(text).toContain('HTTP 403');
    expect(text).not.toContain('需要在官方页面完成额外验证');
  });
  it('未配置登录器时禁用两处2FA，但手工BPS授权独立可用', async () => {
    mocks.capabilities.mockResolvedValue({
      password_login_configured: false,
      bps_login_mode: 'builtin',
    });
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
    let resolve!: (value: { password_login_configured: boolean; bps_login_mode: string }) => void;
    mocks.capabilities.mockReturnValue(new Promise((done) => (resolve = done)));
    await render();
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    expect(JSON.stringify(renderer.toJSON())).toContain('正在确认2FA登录器配置');
    await submit();
    expect(mocks.create).not.toHaveBeenCalled();
    await act(async () => resolve({ password_login_configured: true, bps_login_mode: 'builtin' }));
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
    let resolveOld!: (value: {
      password_login_configured: boolean;
      bps_login_mode: string;
    }) => void;
    mocks.capabilities.mockReturnValueOnce(new Promise((done) => (resolveOld = done)));
    await render();
    mocks.capabilities.mockReturnValue(new Promise(() => {}));
    await act(async () =>
      renderer.update(
        <InternalLoginCards scope={{ apiBase: 'https://other.invalid', managementKey: 'other' }} />
      )
    );
    await act(async () =>
      resolveOld({ password_login_configured: true, bps_login_mode: 'builtin' })
    );
    expect(createButtons().every((button) => button.props.disabled)).toBe(true);
    expect(JSON.stringify(renderer.toJSON())).toContain('正在确认2FA登录器配置');
  });
  it('未配置登录器不阻断已有批次查询和取消', async () => {
    mocks.capabilities.mockResolvedValue({
      password_login_configured: false,
      bps_login_mode: 'builtin',
    });
    mocks.active.mockResolvedValue(running);
    mocks.cancel.mockResolvedValue({ ...running, status: 'cancelling' });
    await render();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
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
    expect(JSON.stringify(renderer.toJSON())).toContain('具体原因待确认');
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
    mocks.status.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });

  const deferred = <T,>() => {
    let resolve!: (value: T) => void;
    let reject!: (error: unknown) => void;
    const promise = new Promise<T>((done, fail) => {
      resolve = done;
      reject = fail;
    });
    return { promise, resolve, reject };
  };
  const bpsButton = (label: string) =>
    renderer.root.findAllByType(Button).find((node) => node.props.children === label)!;
  async function startBPS() {
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/authorize', state: 'state' });
    mocks.status.mockResolvedValue({ status: 'wait' });
    mocks.callback.mockResolvedValue({ status: 'ok' });
    await render();
    await act(async () => {
      await bpsButton('生成授权链接').props.onClick();
    });
    callbacks[0].value = 'https://callback.invalid/?state=state&code=CODE_CANARY';
  }
  it('专项：慢查询最大在途数为一', async () => {
    await startBPS();
    const pending = deferred<{ status: string }>();
    mocks.status.mockReturnValue(pending.promise);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(mocks.status).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve({ status: 'wait' }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(mocks.status).toHaveBeenCalledTimes(2);
  });
  it('专项：旧查询missing不清提交中会话，ACK后只读恢复', async () => {
    await startBPS();
    const poll = deferred<unknown>();
    const post = deferred<unknown>();
    mocks.status.mockReturnValueOnce(poll.promise);
    mocks.callback.mockReturnValueOnce(post.promise);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    let submitPromise!: Promise<void>;
    await act(async () => {
      submitPromise = bpsButton('提交回调').props.onClick();
    });
    await act(async () =>
      poll.resolve({ status: 'error', error_code: 'session_missing_or_expired' })
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4500);
    });
    expect(mocks.status).toHaveBeenCalledTimes(1);
    expect(bpsButton('提交回调')).toBeDefined();
    await act(async () => {
      post.resolve({ status: 'ok' });
      await submitPromise;
    });
    mocks.status.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('账号已保存并注册');
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
  it.each(['在途', 'ACK后'] as const)('专项：提交%s卸载不DELETE', async (when) => {
    await startBPS();
    const post = deferred<unknown>();
    mocks.callback.mockReturnValueOnce(post.promise);
    let submitPromise!: Promise<void>;
    await act(async () => {
      submitPromise = bpsButton('提交回调').props.onClick();
    });
    if (when === 'ACK后')
      await act(async () => {
        post.resolve({ status: 'ok' });
        await submitPromise;
      });
    await act(async () => renderer.unmount());
    expect(mocks.cancelOAuth).not.toHaveBeenCalled();
    if (when === '在途')
      await act(async () => {
        post.resolve({ status: 'ok' });
        await submitPromise;
      });
  });
  it('专项：取消在途禁止新建，迟到取消不跨连接清新会话', async () => {
    await startBPS();
    const oldCancel = deferred<unknown>();
    mocks.cancelOAuth.mockReturnValueOnce(oldCancel.promise);
    let cancelPromise!: Promise<void>;
    await act(async () => {
      cancelPromise = bpsButton('取消').props.onClick();
    });
    mocks.status.mockResolvedValue({ status: 'error', error_code: 'session_missing_or_expired' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(bpsButton('生成授权链接').props.disabled).toBe(true);
    const nextScope = { apiBase: 'https://next.invalid', managementKey: 'next' };
    await act(async () => renderer.update(<InternalLoginCards key="next" scope={nextScope} />));
    mocks.startBPS.mockResolvedValue({ url: 'https://auth.invalid/B', state: 'B' });
    await act(async () => {
      await bpsButton('生成授权链接').props.onClick();
    });
    await act(async () => {
      oldCancel.resolve({ status: 'ok', cancelled: true });
      await cancelPromise;
    });
    expect(renderer.root.findByType('a').props.href).toBe('https://auth.invalid/B');
  });
  it.each([
    {},
    { saved: true },
    { saved: true, adopted: false },
    { saved: false, adopted: true },
    { saved: 'true', adopted: 'true' },
  ])('专项：不完整保存事实%j保持只读未知', async (facts) => {
    await startBPS();
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    mocks.status.mockResolvedValue({ status: 'ok', ...facts });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(JSON.stringify(renderer.toJSON())).not.toContain('账号已保存并注册');
    expect(bpsButton('查询结果')).toBeDefined();
    expect(mocks.publish).not.toHaveBeenCalled();
  });
  it('专项：持续404暂停轮询，手动GET后双true仅通知一次', async () => {
    await startBPS();
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    mocks.status.mockRejectedValue({
      status: 404,
      data: { error_code: 'session_missing_or_expired', error_source: 'manager_session' },
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(mocks.status).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(renderer.toJSON())).toContain('来源：Manager会话');
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    expect(mocks.status).toHaveBeenCalledTimes(2);
    mocks.status.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(mocks.status).toHaveBeenCalledTimes(3);
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.startBPS).toHaveBeenCalledTimes(1);
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
  it('专项：callback ACK丢失保留只读查询，不重放callback', async () => {
    await startBPS();
    mocks.callback.mockRejectedValueOnce({ status: 502, data: { error_source: 'transport' } });
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(mocks.status).not.toHaveBeenCalled();
    mocks.status.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
  it('专项：未提交取消只接受真实cancelled，false保留查询', async () => {
    await startBPS();
    mocks.cancelOAuth.mockResolvedValueOnce({ status: 'ok', cancelled: false });
    await act(async () => {
      await bpsButton('取消').props.onClick();
    });
    expect(bpsButton('查询结果')).toBeDefined();
    expect(JSON.stringify(renderer.toJSON())).not.toContain('授权会话已取消');
  });
  it('专项：未提交真实取消允许新建，已提交停止观察不DELETE', async () => {
    await startBPS();
    await act(async () => {
      await bpsButton('取消').props.onClick();
    });
    expect(JSON.stringify(renderer.toJSON())).toContain('授权会话已取消');
    expect(bpsButton('生成授权链接').props.disabled).toBe(false);
    await act(async () => {
      await bpsButton('生成授权链接').props.onClick();
    });
    callbacks[callbacks.length - 1].value =
      'https://callback.invalid/?state=state&code=CODE_CANARY';
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    await act(async () => {
      bpsButton('停止观察').props.onClick();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(mocks.cancelOAuth).toHaveBeenCalledTimes(1);
    expect(mocks.status).not.toHaveBeenCalled();
    expect(bpsButton('查询结果')).toBeDefined();
  });
  it('专项：查询响应丢失后手动查询保持单在途，不重放callback', async () => {
    await startBPS();
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    mocks.status.mockRejectedValueOnce({ status: 502, data: { error_source: 'transport' } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    const pending = deferred<unknown>();
    mocks.status.mockReturnValueOnce(pending.promise);
    let queryPromise!: Promise<void>;
    await act(async () => {
      queryPromise = bpsButton('查询结果').props.onClick();
    });
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    expect(mocks.status).toHaveBeenCalledTimes(2);
    await act(async () => {
      pending.resolve({ status: 'ok', saved: true, adopted: true });
      await queryPromise;
    });
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.publish).toHaveBeenCalledTimes(1);
  });
  it('P2：未提交missing允许重新开始B，A迟到查询不影响B', async () => {
    await startBPS();
    mocks.status.mockRejectedValueOnce({ status: 404 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(bpsButton('重新开始授权')).toBeDefined();
    const oldQuery = deferred<unknown>();
    mocks.status.mockReturnValueOnce(oldQuery.promise);
    let queryPromise!: Promise<void>;
    await act(async () => {
      queryPromise = bpsButton('查询结果').props.onClick();
    });
    mocks.startBPS.mockResolvedValueOnce({ url: 'https://auth.invalid/B', state: 'B' });
    await act(async () => {
      await bpsButton('重新开始授权').props.onClick();
    });
    await act(async () => {
      oldQuery.resolve({ status: 'error', error_code: 'session_missing_or_expired' });
      await queryPromise;
    });
    expect(renderer.root.findByType('a').props.href).toBe('https://auth.invalid/B');
    expect(bpsButton('提交回调').props.disabled).toBe(false);
    expect(mocks.callback).not.toHaveBeenCalled();
    expect(mocks.startBPS).toHaveBeenCalledTimes(2);
  });
  it('P2：未提交临时查询失败后wait恢复原会话回调输入', async () => {
    await startBPS();
    mocks.status.mockRejectedValueOnce({ status: 502, data: { error_source: 'transport' } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    mocks.status.mockResolvedValueOnce({ status: 'wait' });
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    expect(bpsButton('提交回调')).toBeDefined();
    expect(bpsButton('提交回调').props.disabled).toBe(false);
    callbacks[callbacks.length - 1].value = 'https://callback.invalid/?state=state&code=RECOVERED';
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    expect(mocks.callback).toHaveBeenCalledExactlyOnceWith(
      'state',
      'https://callback.invalid/?state=state&code=RECOVERED',
      scope
    );
    expect(mocks.startBPS).toHaveBeenCalledTimes(1);
  });
  it('P2：已提交missing及手动wait仍禁止重新授权或重放回调', async () => {
    await startBPS();
    await act(async () => {
      await bpsButton('提交回调').props.onClick();
    });
    mocks.status.mockRejectedValueOnce({ status: 404 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(bpsButton('重新开始授权')).toBeUndefined();
    expect(bpsButton('生成授权链接').props.disabled).toBe(true);
    await act(async () => {
      await bpsButton('生成授权链接').props.onClick();
    });
    mocks.status.mockResolvedValueOnce({ status: 'wait' });
    await act(async () => {
      await bpsButton('查询结果').props.onClick();
    });
    expect(bpsButton('提交回调')).toBeUndefined();
    expect(bpsButton('重新开始授权')).toBeUndefined();
    expect(mocks.callback).toHaveBeenCalledTimes(1);
    expect(mocks.startBPS).toHaveBeenCalledTimes(1);
  });
});
