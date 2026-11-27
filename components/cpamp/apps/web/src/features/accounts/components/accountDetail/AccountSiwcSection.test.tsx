import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { buildAccountRows } from '../../model/accountRows';
import { Button } from '@/components/ui/Button';
import { AccountSiwcSection } from './AccountSiwcSection';
const api = vi.hoisted(() => ({
  credential: vi.fn(),
  bind: vi.fn(),
  callback: vi.fn(),
  status: vi.fn(),
  cancel: vi.fn(),
}));
vi.mock('@/services/api/siwc', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  siwcApi: api,
}));
vi.mock('react-i18next', async (importOriginal) => ({ ...await importOriginal<typeof import('react-i18next')>(), useTranslation: () => ({ t: (key: string) => key }) }));
const scope = { apiBase: 'https://panel.invalid', managementKey: 'synthetic' };
const row = buildAccountRows(
  [
    {
      id: 'auth-a',
      name: 'synthetic.json',
      source: 'file',
      auth_index: 'index-a',
      provider: 'codex',
      type: 'codex',
    },
  ],
  {
    antigravityQuota: {},
    claudeQuota: {},
    codexQuota: {},
    devinQuota: {},
    kimiQuota: {},
    metaQuota: {},
    xaiQuota: {},
  }
)[0];
const credential = {
  auth_id: 'auth-a',
  auth_index: 'index-a',
  connection_ref: 'connection-a',
  provider: 'codex',
  siwc_bind_supported: true,
  use_siwc: false,
  siwc_bound: false,
  siwc_status: 'off',
};
const session = {
  ...credential,
  status: 'wait',
  flow: 'siwc_bind',
  url: 'https://auth.openai.com/authorize?state=issued-state',
  state: 'issued-state',
  expires_in: 300,
};
const confirmed = { ...credential, siwc_bound: true, use_siwc: true, siwc_status: 'valid' };
describe('账号详情 SIWC 生命周期', () => {
  let renderer: ReactTestRenderer | undefined;
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    api.credential.mockResolvedValue(credential);
    api.bind.mockResolvedValue(session);
    api.callback.mockResolvedValue({ status: 'wait', submitted: true });
    api.status.mockResolvedValue({ status: 'wait' });
    api.cancel.mockResolvedValue({ status: 'cancelled', cancelled: true });
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer?.unmount());
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  const text = () => JSON.stringify(renderer!.toJSON());
  const button = (label: string) =>
    renderer!.root.findAllByType(Button).find((b) => b.props.children === label)!;
  const mount = async (props = {}) => {
    await act(async () => {
      renderer = create(
        <AccountSiwcSection row={row} scope={scope} home={false} disabled={false} {...props} />
      );
    });
  };
  const start = async () => {
    await act(async () => {
      await button('siwc.bind').props.onClick();
    });
  };
  it('wait、提交回调保持未完成，只有 ok+回读显示成功并清除敏感输入', async () => {
    await mount();
    await start();
    expect(text()).toContain('siwc.waiting');
    expect(text()).not.toContain('siwc.success');
    const redirect =
      'http://127.0.0.1:1455/auth/callback?state=issued-state&code=issued-code&client_id=issued-client';
    await act(async () => {
      renderer!.root.findAllByType('input')[1].props.onChange({ target: { value: redirect } });
    });
    await act(async () => {
      renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(api.callback).toHaveBeenCalledWith(
      session,
      session.state,
      redirect,
      scope,
      expect.any(AbortSignal)
    );
    expect(text()).toContain('siwc.submitted');
    expect(text()).not.toContain('issued-code');
    expect(text()).not.toContain('siwc.success');
    api.status.mockResolvedValue({ status: 'ok', credential: confirmed });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(text()).toContain('siwc.success');
    expect(text()).toContain('siwc.status_valid');
    expect(text()).not.toContain('issued-state');
  });
  it('取消已输给提交时保持等待，后续已确认成功才能显示成功', async () => {
    await mount();
    await start();
    api.cancel.mockResolvedValue({ status: 'wait', cancelled: false });
    await act(async () => {
      await button('siwc.cancel').props.onClick();
    });
    expect(text()).toContain('siwc.cancel_pending');
    expect(text()).not.toContain('siwc.cancelled');
    expect(text()).not.toContain('siwc.success');
    api.status.mockResolvedValue({ status: 'ok', credential: confirmed });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(text()).toContain('siwc.success');
  });
  it('切连接取消原state并丢弃原连接迟到终态', async () => {
    await mount();
    await start();
    let finish!: (value: unknown) => void;
    api.status.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        })
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    const nextScope = { ...scope, apiBase: 'https://next.invalid' };
    api.credential.mockResolvedValue({ ...credential, connection_ref: 'connection-b' });
    await act(async () => {
      renderer!.update(
        <AccountSiwcSection row={row} scope={nextScope} home={false} disabled={false} />
      );
    });
    expect(api.cancel).toHaveBeenCalledWith(session, session.state, scope);
    await act(async () => finish({ status: 'ok', credential: confirmed }));
    expect(text()).not.toContain('siwc.success');
    expect(text()).not.toContain('issued-state');
  });
  it('卸载后迟到的新建会话按原连接取消', async () => {
    let finish!: (value: unknown) => void;
    api.bind.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        })
    );
    await mount();
    await start();
    await act(async () => renderer!.unmount());
    renderer = undefined;
    await act(async () => finish(session));
    expect(api.cancel).toHaveBeenCalledWith(session, session.state, scope);
  });
  it('五分钟截止只取消当前会话，未确认不报成功', async () => {
    await mount();
    await start();
    api.cancel.mockResolvedValue({ status: 'wait', cancelled: false });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000);
    });
    expect(api.cancel).toHaveBeenCalledTimes(1);
    expect(text()).toContain('siwc.expired_unconfirmed');
    expect(text()).not.toContain('siwc.success');
    const calls = api.status.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(api.status).toHaveBeenCalledTimes(calls);
  });
  it('服务端到期410清除旧会话并允许原位重新绑定', async () => {
    await mount();
    await start();
    api.cancel.mockRejectedValue({ status: 410, data: { code: 'siwc_session_expired' } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300_000);
    });
    expect(text()).toContain('siwc.expired');
    expect(text()).not.toContain('issued-state');
    expect(button('siwc.bind').props.disabled).toBe(false);
    await start();
    expect(api.bind).toHaveBeenCalledTimes(2);
  });
  it.each([
    [{ status: 400, data: { code: 'siwc_callback_invalid' } }, false, 'siwc.callback_invalid'],
    [new Error('response lost'), true, 'siwc.request_failed'],
    [{ status: 502 }, true, 'siwc.request_failed'],
  ])('明确输入拒绝可纠正，响应不明仅查状态', async (error, submitted, message) => {
    await mount();
    await start();
    api.callback.mockRejectedValue(error);
    await act(async () => {
      renderer!.root
        .findAllByType('input')[1]
        .props.onChange({
          target: {
            value: 'http://127.0.0.1:1455/auth/callback?state=issued-state&code=issued-code',
          },
        });
    });
    await act(async () => {
      renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(text()).toContain(message);
    expect(renderer!.root.findAllByType('input')[1].props.disabled).toBe(submitted);
    expect(text()).not.toContain('issued-code');
    expect(text()).not.toContain('siwc.success');
  });
  it('错state回调可重新输入', async () => {
    await mount(); await start();
    api.callback.mockRejectedValue({ status: 400, data: { code: 'siwc_callback_state_mismatch' } });
    await act(async () => { renderer!.root.findAllByType('input')[1].props.onChange({ target: { value: 'http://127.0.0.1:1455/auth/callback?state=other-state&code=issued-code' } }); });
    await act(async () => { renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} }); });
    expect(text()).toContain('siwc.callback_invalid'); expect(renderer!.root.findAllByType('input')[1].props.disabled).toBe(false);
  });
  it('服务端Home摘要覆盖本地未知模式并只读', async () => {
    api.credential.mockResolvedValue({
      ...credential,
      siwc_bind_supported: false,
      read_only_reason: 'home_managed',
    });
    await mount();
    expect(button('siwc.bind').props.disabled).toBe(true);
    expect(text()).toContain('siwc.home_readonly');
  });
  it.each([
    { home: true },
    { row: { ...row, runtimeOnly: true } },
    { row: { ...row, raw: { ...row.raw, source: 'config' } } },
  ])('特殊来源只读不发绑定请求', async (props) => {
    await mount(props);
    expect(button('siwc.bind').props.disabled).toBe(true);
    await start();
    expect(api.bind).not.toHaveBeenCalled();
  });
});
