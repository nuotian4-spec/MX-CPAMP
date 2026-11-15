import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { KiloQuotaPanel } from './KiloQuotaPanel';
import type { KiloQuotaSources, KiloQuotaView } from '@/services/api/kiloQuota';
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  getPolicy: vi.fn(),
  putPolicy: vi.fn(),
  sources: vi.fn(),
  put: vi.fn(),
  remove: vi.fn(),
  query: vi.fn(),
}));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: mocks,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'ADMIN_FIXTURE' },
  reference = 'a'.repeat(32),
  accountID = 'kilo-account';
const empty: KiloQuotaView = {
  account_id: accountID,
  binding_revision: 0,
  connection_ref: reference,
  source: { kind: 'none', status: 'none' },
  query: { status: 'never', attempted_at: null, observed_at: null, error_code: null },
  plan: null,
  windows: [],
  credits: { remaining: null, source_field: null, unit: null },
  stale: false,
};
const bound: KiloQuotaView = {
  ...empty,
  binding_revision: 3,
  source: { kind: 'cpa_codex', credential_id: 'codex-A', status: 'ready' },
};
const success: KiloQuotaView = {
  ...bound,
  query: {
    status: 'success',
    attempted_at: '2026-11-13T02:00:00Z',
    observed_at: '2026-11-13T02:00:00Z',
    error_code: null,
  },
  plan: 'plus',
  windows: [
    {
      group: 'rate_limit',
      slot: 'secondary_window',
      duration_seconds: 18000,
      remaining_percent: 75,
      reset_at: null,
      source: 'api_query',
    },
    {
      group: 'additional_rate_limits:0',
      slot: 'primary_window',
      duration_seconds: 604800,
      remaining_percent: null,
      reset_at: '2026-11-20T00:00:00Z',
      source: 'api_query',
    },
    {
      group: 'additional_rate_limits:0',
      slot: 'secondary_window',
      duration_seconds: 2592000,
      remaining_percent: 0,
      reset_at: null,
      source: 'api_query',
    },
  ],
  credits: { remaining: 12, source_field: 'credits.balance', unit: null },
};
const sources: KiloQuotaSources = {
  connection_ref: reference,
  sources: [
    {
      credential_id: 'codex-A',
      label: '同名来源',
      chatgpt_account_id: 'outer-A',
      status: 'refresh_required',
      has_query_material: false,
    },
    {
      credential_id: 'codex-B',
      label: '同名来源',
      chatgpt_account_id: 'outer-B',
      status: 'ready',
      has_query_material: true,
    },
    {
      credential_id: 'codex-C',
      label: '未知身份',
      chatgpt_account_id: null,
      status: 'identity_unknown',
      has_query_material: false,
    },
  ],
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
describe('Kilo额度面板', () => {
  let renderer: ReactTestRenderer | undefined;
  const onChanged = vi.fn(),
    onClose = vi.fn(),
    storage = { setItem: vi.fn() };
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.getPolicy.mockResolvedValue({
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
      connection_ref: reference,
    });
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('sessionStorage', storage);
    mocks.get.mockResolvedValue(empty);
    mocks.sources.mockResolvedValue(sources);
    mocks.put.mockResolvedValue({ ...bound, binding_revision: 1 });
    mocks.remove.mockResolvedValue({ ...empty, binding_revision: 4 });
    mocks.query.mockResolvedValue({ view: success, cancelled: false });
    onChanged.mockResolvedValue(undefined);
  });
  afterEach(async () => {
    if (renderer) await act(async () => renderer!.unmount());
    renderer = undefined;
    vi.unstubAllGlobals();
  });
  async function render() {
    await act(async () => {
      renderer = create(
        <KiloQuotaPanel
          accountID={accountID}
          scope={scope}
          connectionRef={reference}
          onClose={onClose}
          onChanged={onChanged}
        />
      );
    });
  }
  const text = () => JSON.stringify(renderer!.toJSON());
  const button = (label: string) =>
    renderer!.root.findAllByType(Button).find((item) => item.props.children === label)!;
  async function click(label: string) {
    expect(button(label)).toBeDefined();
    expect(button(label).props.disabled).not.toBe(true);
    await act(async () => {
      button(label).props.onClick();
    });
  }
  async function select(label: string, value: string) {
    await act(async () => {
      const target =
        label === '额度来源类型'
          ? renderer!.root.findByProps({ 'data-source-mode': value })
          : renderer!.root.findByProps({ 'data-source-id': value });
      if (!target.props.disabled) target.props.onClick();
    });
  }
  async function input(label: string, value: string) {
    await act(async () =>
      renderer!.root
        .findAllByType(Input)
        .find((item) => item.props.label === label)!
        .props.onChange({ target: { value } })
    );
  }
  async function submit() {
    await act(async () => {
      await renderer!.root
        .findByProps({ 'aria-label': '额度来源绑定' })
        .props.onSubmit({ preventDefault() {} });
    });
  }
  it('打开读取当前观测与本地来源列表，不查询上游usage，未知不补零或无限', async () => {
    await render();
    expect(mocks.get).toHaveBeenCalledWith(accountID, scope, expect.any(AbortSignal));
    expect(mocks.sources).toHaveBeenCalledWith(scope, expect.any(AbortSignal));
    expect(mocks.query).not.toHaveBeenCalled();
    expect(text()).toContain('未知');
    expect(text()).not.toContain('无限');
    expect(button('查询一次额度').props.disabled).toBe(true);
  });
  it('同名按精确AuthID及outer身份选择，未知身份禁绑定，待续期仍可选', async () => {
    await render();
    await click('读取可引用来源');
    const options = renderer!.root.findAllByProps({ role: 'radio' });
    expect(options).toHaveLength(3);
    expect(options[0].props['data-source-id']).toBe('codex-A');
    expect(options[1].props['data-source-id']).toBe('codex-B');
    expect(options[2].props.disabled).toBe(true);
    await select('精确Codex来源', 'codex-C');
    expect(button('保存额度来源').props.disabled).toBe(true);
    await select('精确Codex来源', 'codex-A');
    expect(text()).toContain('原生命周期尝试续期');
    await submit();
    expect(mocks.put.mock.calls[0].slice(0, 5)).toEqual([
      accountID,
      { kind: 'cpa_codex', credential_id: 'codex-A', chatgpt_account_id: 'outer-A' },
      0,
      reference,
      scope,
    ]);
    expect(mocks.query).not.toHaveBeenCalled();
    expect(text()).toContain('额度来源已保存');
  });
  it('来源搜索仅过滤候选，不查询额度或改写绑定', async () => {
    await render();
    await click('读取可引用来源');
    await input('搜索 Codex 凭证', 'codex-B');
    const options = renderer!.root.findAllByProps({ role: 'radio' });
    expect(options).toHaveLength(1);
    expect(options[0].props['data-source-id']).toBe('codex-B');
    expect(text()).toContain('候选套餐与额度快照：未知');
    expect(mocks.query).not.toHaveBeenCalled();
    expect(mocks.put).not.toHaveBeenCalled();
  });
  it('独立access只写、提交即清空、单在途、不持久化；来源切换清输入', async () => {
    await render();
    await select('额度来源类型', 'imported');
    await input('独立access token（仅写入）', 'SECRET_ACCESS');
    await input('outer账号ID（可选）', 'outer-import');
    const pending = deferred<KiloQuotaView>();
    mocks.put.mockReturnValueOnce(pending.promise);
    let saving!: Promise<void>;
    await act(async () => {
      saving = renderer!.root
        .findByProps({ 'aria-label': '额度来源绑定' })
        .props.onSubmit({ preventDefault() {} });
    });
    expect(
      renderer!.root.findAllByType(Input).find((item) => item.props.type === 'password')!.props
        .value
    ).toBe('');
    await submit();
    expect(mocks.put).toHaveBeenCalledTimes(1);
    expect(mocks.put.mock.calls[0][1]).toEqual({
      kind: 'imported',
      access_token: 'SECRET_ACCESS',
      chatgpt_account_id: 'outer-import',
    });
    await act(async () => {
      pending.resolve({
        ...empty,
        binding_revision: 1,
        source: { kind: 'imported', status: 'ready' },
      });
      await saving;
    });
    expect(storage.setItem).not.toHaveBeenCalled();
    await input('独立access token（仅写入）', 'SECRET_DRAFT');
    await select('额度来源类型', 'cpa_codex');
    await select('额度来源类型', 'imported');
    expect(text()).not.toContain('SECRET_DRAFT');
  });
  it('来源保存成功后列表刷新失败仍显示成功，不自动查询', async () => {
    onChanged.mockRejectedValueOnce(new Error('SECRET_LIST'));
    await render();
    await click('读取可引用来源');
    await select('精确Codex来源', 'codex-A');
    await submit();
    expect(text()).toContain('额度来源已保存');
    expect(text()).toContain('账号列表待刷新');
    expect(text()).not.toContain('SECRET_LIST');
    expect(mocks.query).not.toHaveBeenCalled();
  });
  it('主/附加窗口按duration识别5h/7d/30d，credits单位未知不显示美元', async () => {
    mocks.get.mockResolvedValue(success);
    await render();
    expect(text()).toContain('主额度窗口');
    expect(text()).toContain('附加额度窗口');
    for (const duration of ['5小时', '7天', '30天']) expect(text()).toContain(duration);
    expect(text()).toContain('单位未知');
    expect(text()).not.toContain('$');
    expect(text()).not.toContain('美元');
    expect(text()).toContain('0%');
    expect(text()).toContain('未知');
  });
  it('200 failed保旧值/成功时间/陈旧，usage401不算成功或管理退出', async () => {
    mocks.get.mockResolvedValue(success);
    mocks.query.mockResolvedValueOnce({
      view: {
        ...success,
        query: {
          ...success.query,
          status: 'failed',
          attempted_at: '2026-11-13T03:00:00Z',
          error_code: 'usage_unauthorized',
        },
        stale: true,
      },
      cancelled: false,
    });
    await render();
    await click('查询一次额度');
    expect(text()).toContain('本次额度查询失败');
    expect(text()).not.toContain('本次额度查询成功');
    expect(text()).toContain('不影响管理登录');
    expect(text()).toContain('陈旧观测');
    expect(text()).toContain('2026-11-13T02:00:00Z');
    expect(text()).toContain('2026-11-13T03:00:00Z');
    expect(text()).toContain('75%');
    expect(onChanged).toHaveBeenCalledTimes(1);
  });
  it('独立额度查询成功通知父列表，刷新失败保留查询成功和最新窗口', async () => {
    mocks.get.mockResolvedValue(bound);
    onChanged.mockRejectedValueOnce(new Error('SECRET_LIST'));
    await render();
    await click('查询一次额度');
    expect(onChanged).toHaveBeenCalledTimes(1);
    expect(text()).toContain('本次额度查询成功');
    expect(text()).toContain('账号列表待刷新');
    expect(text()).toContain('75%');
    expect(text()).not.toContain('SECRET_LIST');
  });
  it('嵌入估算动作时只保留同一绑定查询入口，窗口在动作之前', async () => {
    mocks.get.mockResolvedValue(bound);
    await act(async () => {
      renderer = create(
        <KiloQuotaPanel
          accountID={accountID}
          scope={scope}
          connectionRef={reference}
          onClose={onClose}
          onChanged={onChanged}
          renderEstimate={(query, disabled) => (
            <Button disabled={disabled} onClick={() => void query()}>
              查询额度
            </Button>
          )}
        />
      );
    });
    expect(button('查询一次额度')).toBeUndefined();
    expect(text().indexOf('额度窗口')).toBeLessThan(text().indexOf('查询额度'));
    expect(mocks.query).not.toHaveBeenCalled();
    await click('查询额度');
    expect(mocks.query).toHaveBeenCalledTimes(1);
    expect(mocks.query.mock.calls[0].slice(0, 4)).toEqual([accountID, 3, reference, scope]);
  });
  it('真正管理401保持管理认证错误，不混成usage失败', async () => {
    mocks.get.mockResolvedValue(bound);
    mocks.query.mockRejectedValueOnce({ status: 401, message: 'SECRET_AUTH' });
    await render();
    await click('查询一次额度');
    expect(text()).toContain('管理认证已失效');
    expect(text()).not.toContain('SECRET_AUTH');
    expect(button('查询一次额度').props.disabled).toBe(true);
  });
  it('499安全View保留旧成功观测并标取消，不称成功', async () => {
    mocks.get.mockResolvedValue(success);
    mocks.query.mockResolvedValueOnce({
      view: {
        ...success,
        query: { ...success.query, status: 'cancelled', error_code: 'usage_cancelled' },
        stale: true,
      },
      cancelled: true,
    });
    await render();
    await click('查询一次额度');
    expect(text()).toContain('查询已取消');
    expect(text()).toContain('75%');
    expect(text()).not.toContain('本次额度查询成功');
  });
  it('本地中止迟到200不发布，单账号query单在途', async () => {
    mocks.get.mockResolvedValue(bound);
    const pending = deferred<{ view: KiloQuotaView; cancelled: boolean }>();
    mocks.query.mockReturnValueOnce(pending.promise);
    await render();
    const queryButton = button('查询一次额度');
    await act(async () => {
      queryButton.props.onClick();
      queryButton.props.onClick();
    });
    expect(mocks.query).toHaveBeenCalledTimes(1);
    const signal = mocks.query.mock.calls[0][4] as AbortSignal;
    await click('中止本地请求');
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve({ view: success, cancelled: false }));
    expect(text()).toContain('服务端结果待核');
    expect(text()).not.toContain('75%');
    expect(mocks.remove).not.toHaveBeenCalled();
  });
  it('409先重读新binding_revision；迟到旧revision响应不覆盖新绑定', async () => {
    mocks.get.mockResolvedValue(bound);
    mocks.query.mockRejectedValueOnce({ status: 409 });
    await render();
    await click('查询一次额度');
    expect(button('查询一次额度').props.disabled).toBe(true);
    mocks.get.mockResolvedValue({
      ...bound,
      binding_revision: 5,
      source: { kind: 'imported', status: 'ready' },
    });
    await click('读取当前观测');
    mocks.query.mockResolvedValueOnce({ view: success, cancelled: false });
    await click('查询一次额度');
    expect(mocks.query.mock.calls[1][1]).toBe(5);
    expect(text()).toContain('绑定版本、来源身份或连接已变化');
    expect(text()).not.toContain('75%');
  });
  it('解除绑定需确认，清当前来源而不删除Codex', async () => {
    mocks.get.mockResolvedValue(bound);
    await render();
    await click('解除额度绑定');
    expect(mocks.remove).not.toHaveBeenCalled();
    await click('保留绑定');
    expect(mocks.remove).not.toHaveBeenCalled();
    await click('解除额度绑定');
    await click('确认解除绑定');
    expect(mocks.remove.mock.calls[0].slice(0, 4)).toEqual([accountID, 3, reference, scope]);
    expect(text()).toContain('Codex来源未删除');
    expect(button('查询一次额度').props.disabled).toBe(true);
  });
  it.each(['account', 'scope', 'reference', 'close'] as const)(
    '%s变化中止旧查询，迟到结果不覆盖',
    async (change) => {
      mocks.get.mockResolvedValue(bound);
      const pending = deferred<{ view: KiloQuotaView; cancelled: boolean }>();
      mocks.query.mockReturnValueOnce(pending.promise);
      await render();
      await click('查询一次额度');
      const signal = mocks.query.mock.calls[0][4] as AbortSignal;
      await act(async () => {
        if (change === 'close') renderer!.unmount();
        else {
          const nextID = change === 'account' ? 'new-account' : accountID;
          const nextRef = change === 'reference' ? 'b'.repeat(32) : reference;
          mocks.get.mockResolvedValue({ ...empty, account_id: nextID, connection_ref: nextRef });
          renderer!.update(
            <KiloQuotaPanel
              accountID={nextID}
              scope={change === 'scope' ? { ...scope, apiBase: 'https://new.invalid' } : scope}
              connectionRef={nextRef}
              onClose={onClose}
              onChanged={onChanged}
            />
          );
        }
      });
      expect(signal.aborted).toBe(true);
      await act(async () => pending.resolve({ view: success, cancelled: false }));
      if (change !== 'close') expect(text()).not.toContain('75%');
      expect(onChanged).not.toHaveBeenCalled();
    }
  );
  it('旧来源列表在账号切换后丢弃，同名不自动选中', async () => {
    await render();
    const pending = deferred<KiloQuotaSources>();
    mocks.sources.mockReturnValueOnce(pending.promise);
    await click('读取可引用来源');
    const signal = mocks.sources.mock.calls[mocks.sources.mock.calls.length - 1][1] as AbortSignal;
    mocks.get.mockResolvedValue({ ...empty, account_id: 'next' });
    mocks.sources.mockResolvedValue({
      connection_ref: reference,
      sources: [{ ...sources.sources[1], label: '新来源' }],
    });
    await act(async () =>
      renderer!.update(
        <KiloQuotaPanel
          accountID="next"
          scope={scope}
          connectionRef={reference}
          onClose={onClose}
          onChanged={onChanged}
        />
      )
    );
    await act(async () => pending.resolve(sources));
    expect(signal.aborted).toBe(true);
    expect(text()).not.toContain('同名来源');
    expect(mocks.put).not.toHaveBeenCalled();
  });
});
