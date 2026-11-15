import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloSourcePolicyFields } from './KiloSourcePolicyFields';
import { KiloSourcePolicyStatus } from './KiloSourcePolicyStatus';
import { KiloRecoveryStatus } from './KiloRecoveryStatus';
import type { KiloAccount } from '@/services/api/kilo';
import type { KiloSourcePolicySnapshot, KiloQuotaView } from '@/services/api/kiloQuota';
const mocks = vi.hoisted(() => ({ getPolicy: vi.fn(), putPolicy: vi.fn() }));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: mocks,
}));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'SYNTHETIC' },
  ref = 'a'.repeat(32);
const policy: KiloSourcePolicySnapshot = {
  enabled: false,
  interval_seconds: 1800,
  revision: 0,
  paused: false,
  pause_generation: 0,
  scope: '',
  status: 'disabled',
  observed_at: null,
  next_query_at: null,
  error_code: null,
  persisted: true,
  connection_ref: ref,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
describe('来源额度策略管理', () => {
  let renderer: ReactTestRenderer | undefined;
  const onChanged = vi.fn(),
    onBusyChange = vi.fn();
  beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    mocks.getPolicy.mockResolvedValue(policy);
    mocks.putPolicy.mockResolvedValue({ ...policy, enabled: true, revision: 1, status: 'pending' });
    onChanged.mockResolvedValue(undefined);
  });
  afterEach(async () => {
    await act(async () => renderer?.unmount());
    renderer = undefined;
    vi.unstubAllGlobals();
  });
  const props = () => ({
    accountID: 'a',
    scope,
    connectionRef: ref,
    disabled: false,
    onChanged,
    onBusyChange,
  });
  const render = async (extra = {}) => {
    await act(async () => {
      renderer = create(<KiloSourcePolicyFields {...props()} {...extra} />);
    });
  };
  const text = () => JSON.stringify(renderer!.toJSON());
  const toggle = (label = '启用来源额度100%停调') =>
    renderer!.root.findAllByType(ToggleSwitch).find((item) => item.props.label === label)!;
  const button = (label: string) =>
    renderer!.root.findAllByType(Button).find((item) => item.props.children === label)!;
  const submit = async () => {
    await act(async () => {
      renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
  };
  it('默认关闭与1800秒，加载只读，不自行根据百分比启用', async () => {
    await render();
    expect(toggle().props.checked).toBe(false);
    expect(renderer!.root.findByType(Input).props.value).toBe('1800');
    expect(mocks.getPolicy).toHaveBeenCalledWith('a', scope, expect.any(AbortSignal));
    expect(mocks.putPolicy).not.toHaveBeenCalled();
    expect(text()).toContain('不表示推理可用');
  });
  it('读取失败只显示未知，禁止保存，重新读取后可操作', async () => {
    mocks.getPolicy.mockRejectedValueOnce({ status: 503 });
    await render();
    expect(text()).toContain('来源策略状态未知');
    expect(button('保存关闭策略').props.disabled).toBe(true);
    await submit();
    expect(mocks.putPolicy).not.toHaveBeenCalled();
    await act(async () => button('重新读取策略').props.onClick());
    expect(button('保存关闭策略').props.disabled).toBe(false);
  });
  it.each(['59', '86401', '60.5', '', '-1'])('周期%s拒绝且不发写请求', async (value) => {
    await render();
    await act(async () => renderer!.root.findByType(Input).props.onChange({ target: { value } }));
    await submit();
    expect(mocks.putPolicy).not.toHaveBeenCalled();
    expect(text()).toContain('60至86400');
  });
  it.each([60, 86400])('合法边界%s携带策略版本与绑定连接，仅写一次', async (seconds) => {
    const waiting = deferred<KiloSourcePolicySnapshot>();
    mocks.putPolicy.mockReturnValue(waiting.promise);
    await render();
    await act(async () => {
      toggle().props.onChange(true);
      renderer!.root.findByType(Input).props.onChange({ target: { value: String(seconds) } });
    });
    await submit();
    await submit();
    expect(mocks.putPolicy).toHaveBeenCalledTimes(1);
    expect(mocks.putPolicy).toHaveBeenCalledWith(
      'a',
      { enabled: true, interval_seconds: seconds, expected_policy_revision: 0 },
      ref,
      scope,
      expect.any(AbortSignal)
    );
    await act(async () =>
      waiting.resolve({
        ...policy,
        enabled: true,
        revision: 1,
        interval_seconds: seconds,
        status: 'pending',
      })
    );
    expect(onChanged).toHaveBeenCalledTimes(1);
    expect(text()).toContain('来源策略已保存');
  });
  it('待核范围才可确认，关闭只清本策略', async () => {
    mocks.getPolicy.mockResolvedValue({
      ...policy,
      enabled: true,
      paused: true,
      status: 'source_pending',
    });
    await render();
    expect(toggle('确认新窗口范围，下次观测重新核实')).toBeDefined();
    await act(async () => toggle('确认新窗口范围，下次观测重新核实').props.onChange(true));
    await submit();
    expect(mocks.putPolicy.mock.calls[0][1].confirm_scope).toBe(true);
    await act(async () => toggle().props.onChange(false));
    expect(text()).toContain('只解除本项来源策略');
    mocks.putPolicy.mockResolvedValue({ ...policy, revision: 2 });
    await submit();
    expect(mocks.putPolicy.mock.calls[1][1]).toEqual({
      enabled: false,
      interval_seconds: 1800,
      expected_policy_revision: 1,
    });
    expect(text()).toContain('人工停用、Kilo实际限制和出口等待保持各自状态');
  });
  it('保存待核或连接冲突不称成功也不自动重放', async () => {
    await render();
    mocks.putPolicy.mockResolvedValue({
      ...policy,
      revision: 1,
      status: 'storage_failed',
      persisted: false,
    });
    await submit();
    expect(text()).toContain('保存待核');
    expect(text()).not.toContain('来源策略已关闭');
    expect(mocks.putPolicy).toHaveBeenCalledTimes(1);
    await act(async () => button('重新读取策略').props.onClick());
    mocks.putPolicy.mockResolvedValue({ ...policy, revision: 1, connection_ref: 'b'.repeat(32) });
    await submit();
    expect(text()).toContain('管理连接已变化');
    expect(button('保存关闭策略').props.disabled).toBe(true);
    expect(mocks.putPolicy).toHaveBeenCalledTimes(2);
  });
  it.each(['account', 'scope', 'reference'])('%s切换丢弃旧读取，使用新连接状态', async (kind) => {
    const waiting = deferred<KiloSourcePolicySnapshot>();
    mocks.getPolicy.mockReturnValueOnce(waiting.promise);
    await render();
    const newScope = { ...scope, managementKey: 'SECOND' };
    const extra =
      kind === 'account'
        ? { accountID: 'b' }
        : kind === 'scope'
          ? { scope: newScope }
          : { connectionRef: 'b'.repeat(32) };
    mocks.getPolicy.mockResolvedValue({
      ...policy,
      connection_ref: kind === 'reference' ? 'b'.repeat(32) : ref,
      interval_seconds: 120,
    });
    await act(async () => renderer!.update(<KiloSourcePolicyFields {...props()} {...extra} />));
    expect(mocks.getPolicy.mock.calls[0][2].aborted).toBe(true);
    await act(async () =>
      waiting.resolve({ ...policy, enabled: true, status: 'paused', paused: true })
    );
    expect(renderer!.root.findByType(Input).props.value).toBe('120');
    expect(toggle().props.checked).toBe(false);
    expect(onChanged).not.toHaveBeenCalled();
  });
  it('额度观测变化后重读服务端策略，不用显示百分比推导暂停', async () => {
    await render();
    mocks.getPolicy.mockResolvedValue({ ...policy, enabled: true, status: 'ready', paused: false });
    await act(async () =>
      renderer!.update(
        <KiloSourcePolicyFields
          {...props()}
          observation={{ windows: [{ used_percent: 100 }] } as KiloQuotaView}
        />
      )
    );
    expect(mocks.getPolicy).toHaveBeenCalledTimes(2);
    expect(text()).toContain('来源策略当前未暂停');
    expect(text()).not.toContain('来源策略暂停仍保留');
  });
  it('实际限制和来源暂停独立显示，未知查询出口不伪造恢复', async () => {
    await act(async () => {
      renderer = create(
        <KiloRecoveryStatus
          account={
            {
              id: 'a',
              name: '合成账号',
              enabled: true,
              priority: 0,
              concurrency_limit: null,
              effective_limit: 20,
              active: 0,
              waiting: 0,
              allow_direct_when_proxy_full: false,
              auto_restriction: {
                reason: 'quota_exhausted',
                query_status: 'never',
                persisted: true,
                generation: 0,
                identity_revision: 0,
                source_revision: 0,
                recover_at: null,
                error_code: null,
              },
              source_policy: {
                ...policy,
                enabled: true,
                paused: true,
                status: 'waiting_query_egress',
                error_code: 'query_egress_unavailable',
              },
            } as KiloAccount
          }
        />
      );
    });
    expect(text()).toContain('额度耗尽限制');
    expect(text()).toContain('来源策略暂停仍保留');
    expect(text()).toContain('等待查询出口');
    expect(text()).not.toContain('推理成功');
  });
  it('原始未知状态不标成关闭或恢复', async () => {
    await act(async () => {
      renderer = create(<KiloSourcePolicyStatus />);
    });
    expect(text()).toContain('来源策略状态未知');
    expect(text()).not.toContain('已关闭');
  });
});
