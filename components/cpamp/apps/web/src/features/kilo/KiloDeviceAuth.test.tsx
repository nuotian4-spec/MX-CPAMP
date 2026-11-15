import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { KiloDeviceAuth } from './KiloDeviceAuth';
import type {
  KiloDeviceSaved,
  KiloDeviceStarted,
  KiloDeviceState,
} from '@/services/api/kiloDevice';
const proxies = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock('@/services/api/kiloProxies', () => ({ kiloProxiesApi: proxies }));
const mocks = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), save: vi.fn(), cancel: vi.fn() }));
vi.mock('@/services/api/kiloDevice', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloDevice')>()),
  kiloDeviceApi: mocks,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'ADMIN_FIXTURE' };
const reference = 'a'.repeat(32);
const started: KiloDeviceStarted = {
  id: 'session 1',
  user_code: 'ABCD-EFGH',
  verification_url: 'https://kilo.ai/device',
  expires_in: 900,
  interval: 3,
  inference_egress_mode: 'pool',
  connection_ref: reference,
};
const state = (
  status: KiloDeviceState['status'],
  fields: Partial<KiloDeviceState> = {}
): KiloDeviceState => ({ status, connection_ref: reference, ...fields });
const saved: KiloDeviceSaved = {
  id: 'kilo-saved.json',
  name: '合成账号',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
  egress_mode: 'pool',
  egress_status: 'pending',
  egress_reason: 'shared_proxy_not_configured',
  connection_ref: reference,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
describe('Kilo设备授权状态机', () => {
  let renderer: ReactTestRenderer | undefined;
  const onSaved = vi.fn();
  const onClose = vi.fn();
  const storage = { setItem: vi.fn() };
  const clipboard = { writeText: vi.fn() };
  const open = vi.fn();
  beforeEach(() => {
    vi.resetAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-13T18:00:00Z'));
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('sessionStorage', storage);
    vi.stubGlobal('navigator', { clipboard });
    vi.stubGlobal('open', open);
    proxies.list.mockResolvedValue({
      revision: 7,
      runtime_revision: 7,
      managed: true,
      proxy_limit: 10,
      proxies: [
        {
          id: 'p1',
          name: '代理一',
          account_id: '',
          host: '127.0.0.1',
          port: '8080',
          enabled: true,
          healthy: true,
          active: 0,
          owner: '',
        },
      ],
      accounts: [],
      recycle: [],
      connection_ref: reference,
    });
    mocks.start.mockResolvedValue(started);
    mocks.poll.mockResolvedValue(state('pending'));
    mocks.save.mockResolvedValue(saved);
    mocks.cancel.mockResolvedValue(state('cancelled'));
    onSaved.mockResolvedValue(undefined);
    clipboard.writeText.mockResolvedValue(undefined);
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer!.unmount());
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(
        <KiloDeviceAuth
          scope={scope}
          connectionRef={reference}
          onSaved={onSaved}
          onClose={onClose}
        />
      );
    });
  }
  const text = () => JSON.stringify(renderer!.toJSON());
  const button = (label: string) =>
    renderer!.root.findAllByType(Button).find((b) => b.props.children === label)!;
  async function click(label: string) {
    expect(button(label)).toBeDefined();
    expect(button(label).props.disabled).not.toBe(true);
    await act(async () => {
      button(label).props.onClick();
    });
  }
  async function input(label: string, value: string) {
    await act(async () =>
      renderer!.root
        .findAllByType(Input)
        .find((i) => i.props.label === label)!
        .props.onChange({ target: { value } })
    );
  }
  async function tick(ms: number) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }
  async function start(authMode: 'pool' | 'direct' = 'pool') {
    await input('账号名称', ' 合成账号 ');
    const proxy = renderer!.root
      .findAllByType(Select)
      .find((item) => item.props.ariaLabel === '授权出口');
    if (proxy) await act(async () => proxy.props.onChange(authMode === 'direct' ? 'direct' : 'p1'));
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    await tick(0);
  }
  async function approve() {
    mocks.poll.mockResolvedValueOnce(
      state('approved', { email: 'fixture@example.invalid', save_expires_in: 300 })
    );
    await render();
    await start();
  }
  it.each([
    [{ status: 503, data: { code: 'device_egress_unavailable' } }, '设备授权尚未启动'],
    [{ status: 503 }, '设备授权启动结果未确认'],
  ])('启动失败保留启动语义，不显示保存待核 %#', async (failure, expected) => {
    mocks.start.mockRejectedValueOnce(failure);
    await render();
    await start();
    expect(text()).toContain(expected);
    expect(text()).not.toContain('保存结果待核');
    expect(mocks.start).toHaveBeenCalledTimes(1);
    expect(mocks.poll).not.toHaveBeenCalled();
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it('表单校验及显式direct，不收Key，链接只展示，复制不持久化', async () => {
    await render();
    await act(async () =>
      renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} })
    );
    expect(mocks.start).not.toHaveBeenCalled();
    expect(text()).toContain('1至256');
    expect(renderer!.root.findAllByType(Input).some((i) => i.props.type === 'password')).toBe(
      false
    );
    await start('direct');
    expect(mocks.start.mock.calls[0].slice(0, 3)).toEqual([
      {
        name: '合成账号',
        priority: 0,
        enabled: true,
        concurrency_limit: null,
        allow_direct_when_proxy_full: false,
        authorization_egress: { mode: 'direct', expected_revision: 7 },
        inference_egress: {
          capacity_mode: 'fixed',
          allocation_mode: 'auto',
          max_ips: 1,
          specified_ips: [],
          mode: 'pool',
          candidate_proxy_ids: null,
          exclusive_proxy_ids: [],
          primary_proxy_id: '',
          expected_revision: 7,
        },
      },
      reference,
      scope,
    ]);
    const link = renderer!.root.findByType('a');
    expect(link.props.href).toBe(started.verification_url);
    expect(link.props.target).toBe('_blank');
    expect(link.props.rel).toBe('noopener noreferrer');
    expect(open).not.toHaveBeenCalled();
    await click('复制验证码');
    expect(clipboard.writeText).toHaveBeenCalledWith('ABCD-EFGH');
    expect(storage.setItem).not.toHaveBeenCalled();
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it('至少3秒轮询且单在途，approved停止轮询并等待显式保存', async () => {
    await render();
    await start();
    expect(mocks.poll).toHaveBeenCalledTimes(1);
    await tick(2999);
    expect(mocks.poll).toHaveBeenCalledTimes(1);
    const pending = deferred<KiloDeviceState>();
    mocks.poll.mockReturnValueOnce(pending.promise);
    await tick(1);
    expect(mocks.poll).toHaveBeenCalledTimes(2);
    await tick(12000);
    expect(mocks.poll).toHaveBeenCalledTimes(2);
    expect(button('核实授权状态').props.disabled).toBe(true);
    await act(async () => pending.resolve(state('approved', { save_expires_in: 300 })));
    expect(text()).toContain('已批准，尚未保存');
    expect(mocks.save).not.toHaveBeenCalled();
    await tick(3000);
    expect(mocks.poll).toHaveBeenCalledTimes(2);
    await click('保存授权账号');
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(mocks.save.mock.calls[0].slice(0, 3)).toEqual(['session 1', reference, scope]);
    expect(onSaved).toHaveBeenCalledWith(saved.id, saved);
    expect(text()).toContain('账号已保存');
    expect(text()).toContain('尚无启用共享代理');
  });
  it('重复查询和重渲染不续300秒保存窗口，到期阻止保存', async () => {
    await approve();
    await tick(100000);
    mocks.poll.mockResolvedValue(state('approved', { save_expires_in: 300 }));
    await click('核实授权状态');
    await act(async () =>
      renderer!.update(
        <KiloDeviceAuth
          scope={scope}
          connectionRef={reference}
          onSaved={onSaved}
          onClose={onClose}
        />
      )
    );
    const windowText = renderer!.root
      .findAllByType('p')
      .find((p) => Array.isArray(p.props.children) && p.props.children.includes('保存窗口剩余 '))!;
    expect(windowText.props.children).toContain(200);
    await tick(200000);
    expect(button('保存授权账号').props.disabled).toBe(true);
    expect(mocks.save).not.toHaveBeenCalled();
    expect(text()).toContain('不代表服务端已取消');
  });
  it('保存503保留批准和原窗口，先核同会话，再显式保存，不按错误正文判成功', async () => {
    await approve();
    mocks.save.mockRejectedValueOnce({ status: 503, message: 'saved=true SECRET_TOKEN' });
    await click('保存授权账号');
    expect(text()).not.toContain('尚未保存');
    expect(text()).toContain('授权已批准，保存结果待核');
    expect(text()).not.toContain('SECRET_TOKEN');
    expect(button('保存授权账号').props.disabled).toBe(true);
    expect(onSaved).not.toHaveBeenCalled();
    await tick(3000);
    mocks.poll.mockResolvedValueOnce(state('approved', { save_expires_in: 297 }));
    await click('核实授权状态');
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(button('保存授权账号').props.disabled).toBe(false);
    await click('保存授权账号');
    expect(mocks.save).toHaveBeenCalledTimes(2);
    expect(onSaved).toHaveBeenCalledTimes(1);
    expect(mocks.start).toHaveBeenCalledTimes(1);
  });
  it('结果待核后poll saved确认稳定AuthID，不重放save；列表失败仍保留成功', async () => {
    await approve();
    mocks.save.mockRejectedValueOnce({ status: 503 });
    onSaved.mockRejectedValueOnce(new Error('LIST_SECRET'));
    await click('保存授权账号');
    await tick(3000);
    mocks.poll.mockResolvedValueOnce(state('saved', { account_id: saved.id }));
    await click('核实授权状态');
    expect(text()).toContain(saved.id);
    expect(text()).toContain('保存已成功，账号列表待刷新');
    expect(text()).not.toContain('LIST_SECRET');
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(mocks.start).toHaveBeenCalledTimes(1);
    expect(onSaved).toHaveBeenCalledTimes(1);
  });
  it.each(['cancelled', 'saved'] as const)('显式服务端取消按真实%s回执展示', async (status) => {
    await approve();
    mocks.cancel.mockResolvedValueOnce(
      state(status, status === 'saved' ? { account_id: saved.id } : {})
    );
    await click('取消服务端授权');
    expect(mocks.cancel.mock.calls[0].slice(0, 3)).toEqual(['session 1', reference, scope]);
    expect(mocks.save).not.toHaveBeenCalled();
    if (status === 'saved') {
      expect(text()).toContain('账号已保存');
      expect(text()).not.toContain('服务端已确认取消');
    } else {
      expect(text()).toContain('服务端已确认取消');
      expect(onSaved).not.toHaveBeenCalled();
    }
  });
  it('取消409不宣称撤销且核实后允许服务端saved', async () => {
    await approve();
    mocks.cancel.mockRejectedValueOnce({ status: 409 });
    await click('取消服务端授权');
    expect(text()).toContain('结果待核');
    expect(text()).not.toContain('服务端已确认取消');
    expect(button('保存授权账号').props.disabled).toBe(true);
    await tick(3000);
    mocks.poll.mockResolvedValueOnce(state('saved', { account_id: saved.id }));
    await click('核实授权状态');
    expect(onSaved).toHaveBeenCalledWith(saved.id, undefined);
  });
  it.each(['denied', 'expired'] as const)('准确展示%s且停止轮询、不自动保存', async (status) => {
    mocks.poll.mockResolvedValueOnce(state(status));
    await render();
    await start();
    expect(text()).toContain(status === 'denied' ? '授权已被拒绝' : '授权会话已过期');
    await tick(9000);
    expect(mocks.poll).toHaveBeenCalledTimes(1);
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it('start和save重复点击均只发送一次', async () => {
    const startPending = deferred<KiloDeviceStarted>();
    mocks.start.mockReturnValueOnce(startPending.promise);
    await render();
    await input('账号名称', '合成账号');
    await act(async () =>
      renderer!.root
        .findAllByType(Select)
        .find((item) => item.props.ariaLabel === '授权出口')!
        .props.onChange('p1')
    );
    let first!: Promise<void>;
    await act(async () => {
      first = renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
      renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.start).toHaveBeenCalledTimes(1);
    mocks.poll.mockResolvedValueOnce(state('approved', { save_expires_in: 300 }));
    await act(async () => {
      startPending.resolve(started);
      await first;
    });
    await tick(0);
    const savePending = deferred<KiloDeviceSaved>();
    mocks.save.mockReturnValueOnce(savePending.promise);
    const saveButton = button('保存授权账号');
    await act(async () => {
      saveButton.props.onClick();
      saveButton.props.onClick();
    });
    expect(mocks.save).toHaveBeenCalledTimes(1);
    await act(async () => savePending.resolve(saved));
  });
  it.each(['scope', 'reference', 'unmount'] as const)(
    '%s变化中止请求并丢弃迟到结果，不执行服务端取消',
    async (change) => {
      await approve();
      const pending = deferred<KiloDeviceSaved>();
      mocks.save.mockReturnValueOnce(pending.promise);
      await click('保存授权账号');
      const signal = mocks.save.mock.calls[0][3] as AbortSignal;
      await act(async () => {
        if (change === 'unmount') renderer!.unmount();
        else
          renderer!.update(
            <KiloDeviceAuth
              scope={change === 'scope' ? { ...scope, apiBase: 'https://next.invalid' } : scope}
              connectionRef={change === 'reference' ? 'b'.repeat(32) : reference}
              onSaved={onSaved}
              onClose={onClose}
            />
          );
      });
      expect(signal.aborted).toBe(true);
      await act(async () => pending.resolve(saved));
      expect(onSaved).not.toHaveBeenCalled();
      expect(mocks.cancel).not.toHaveBeenCalled();
      if (change !== 'unmount') expect(text()).not.toContain(saved.id);
    }
  );
  it('关闭只交还父组件卸载，不自动DELETE；卸载后停止轮询', async () => {
    await render();
    await start();
    await click('关闭窗口');
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mocks.cancel).not.toHaveBeenCalled();
    await act(async () => renderer!.unmount());
    await tick(9000);
    expect(mocks.poll).toHaveBeenCalledTimes(1);
  });
  it.each([
    ['shared_proxy_not_configured', '尚无启用共享代理'],
    ['shared_proxy_unverified', '尚待健康检查'],
    ['shared_proxy_unhealthy', '共享出口故障'],
    ['shared_proxy_capacity', '容量或归属等待中'],
  ] as const)('已保存出口%s如实显示', async (reason, message) => {
    mocks.save.mockResolvedValueOnce({ ...saved, egress_reason: reason });
    await approve();
    await click('保存授权账号');
    expect(text()).toContain(message);
    expect(text()).toContain('凭证保存与出口配齐分别确认');
    expect(text()).toContain('订阅绑定和模型可用性以各自结果为准');
  });
  it('poll失败停止自动轮询，错误正文不显示，显式查询才继续', async () => {
    mocks.poll.mockRejectedValueOnce({ status: 429, message: 'SECRET_TOKEN' });
    await render();
    await start();
    await tick(9000);
    expect(mocks.poll).toHaveBeenCalledTimes(1);
    expect(text()).not.toContain('SECRET_TOKEN');
    await click('核实授权状态');
    expect(mocks.poll).toHaveBeenCalledTimes(2);
  });
  it('回执connection_ref变化视为结果待核，不接受已保存', async () => {
    await approve();
    mocks.save.mockResolvedValueOnce({ ...saved, connection_ref: 'b'.repeat(32) });
    await click('保存授权账号');
    expect(text()).toContain('结果待核');
    expect(onSaved).not.toHaveBeenCalled();
    expect(text()).not.toContain(saved.id);
  });
  it('批准后的状态查询失败也要求重新核实，不复用旧批准直接保存', async () => {
    await approve();
    await tick(3000);
    mocks.poll.mockRejectedValueOnce({ status: 409 });
    await click('核实授权状态');
    expect(button('保存授权账号').props.disabled).toBe(true);
    expect(mocks.save).not.toHaveBeenCalled();
    await tick(3000);
    mocks.poll.mockResolvedValueOnce(state('approved', { save_expires_in: 294 }));
    await click('核实授权状态');
    expect(button('保存授权账号').props.disabled).toBe(false);
  });
  it('出口存储待核的完整回执传回单页，保留已保存ID且不重复save', async () => {
    const uncertain: KiloDeviceSaved = {
      ...saved,
      credential_saved: true,
      egress_ready: false,
      egress_error: 'storage_failed',
      egress_reason: 'storage_failed',
    };
    mocks.save.mockResolvedValueOnce(uncertain);
    await approve();
    await click('保存授权账号');
    expect(onSaved).toHaveBeenCalledWith(saved.id, uncertain);
    await tick(6000);
    expect(mocks.save).toHaveBeenCalledTimes(1);
  });
  it('凭证保存成功与指定IP等待分开确认，等待不重复保存', async () => {
    mocks.save.mockResolvedValue({
      ...saved,
      credential_saved: true,
      egress_ready: false,
      egress_error: 'specified_conflict',
    });
    await approve();
    await click('保存授权账号');
    expect(text()).toContain('账号已保存');
    expect(text()).toContain('指定 IP 已被占用');
    expect(text()).not.toContain('出口已配齐');
    await tick(6000);
    expect(mocks.save).toHaveBeenCalledTimes(1);
    expect(onSaved).toHaveBeenCalledTimes(1);
  });
});
