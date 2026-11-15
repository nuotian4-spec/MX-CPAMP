import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloAccountsPanel } from './KiloAccountsPage';
import type { KiloAccount } from '@/services/api/kilo';
const mocks = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn() }));
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
vi.mock('@/services/api/kilo', async (original) => ({
  ...(await original<typeof import('@/services/api/kilo')>()),
  kiloApi: mocks,
}));
vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'KEY' },
  ref = 'a'.repeat(32);
const account: KiloAccount = {
  id: 'a',
  name: '账号a',
  enabled: true,
  priority: 0,
  concurrency_limit: null,
  effective_limit: 20,
  active: 0,
  waiting: 0,
  allow_direct_when_proxy_full: false,
  auto_restriction: {
    reason: 'quota_exhausted',
    generation: 3,
    identity_revision: 1,
    source_revision: 1,
    recover_at: null,
    query_status: 'no_source',
    error_code: null,
    persisted: true,
  },
};
const restored = {
  ...account,
  connection_ref: ref,
  auto_restriction: { ...account.auto_restriction!, reason: 'none' as const, generation: 4 },
};
const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve();
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
describe('Kilo人工状态与自动恢复投影', () => {
  let renderer: ReactTestRenderer | undefined;
  beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    mocks.list.mockResolvedValue({ accounts: [account], connection_ref: ref });
    mocks.update.mockResolvedValue(restored);
  });
  afterEach(async () => {
    await act(async () => renderer?.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  const render = async () => {
    await act(async () => {
      renderer = create(
        <MemoryRouter>
          <KiloAccountsPanel scope={scope} />
        </MemoryRouter>
      );
    });
  };
  const text = () => JSON.stringify(renderer!.toJSON());
  const click = async (label: string) => {
    await act(async () => {
      renderer!.root
        .findAllByType(Button)
        .find((item) => item.props.children === label)!
        .props.onClick();
      await flush();
    });
  };
  it('人工启用与额度限制分开，无来源保持无计划', async () => {
    await render();
    expect(text()).toContain('已启用');
    expect(text()).toContain('额度耗尽限制');
    expect(text()).toContain('未绑定额度来源');
    expect(text()).toContain('尚无自动解除计划');
  });
  it.each([
    ['running', '正在查询恢复时间'],
    ['failed', '恢复时间查询失败'],
    ['no_future_reset', '未取得未来重置时间'],
    ['cancelled', '恢复时间查询已取消'],
  ] as const)('%s状态准确展示', async (status, label) => {
    mocks.list.mockResolvedValue({
      accounts: [
        {
          ...account,
          auto_restriction: {
            ...account.auto_restriction,
            query_status: status,
            error_code: 'usage_forbidden',
            persisted: false,
          },
        },
      ],
      connection_ref: ref,
    });
    await render();
    expect(text()).toContain(label);
    expect(text()).toContain('尚未持久化');
    expect(text()).toContain('额度上游拒绝访问');
  });
  it('到期仍只显示Core计划，不用本地时钟解除', async () => {
    vi.useFakeTimers();
    mocks.list.mockResolvedValue({
      accounts: [
        {
          ...account,
          auto_restriction: {
            ...account.auto_restriction,
            recover_at: '2020-01-01T00:00:00Z',
            query_status: 'success',
          },
        },
      ],
      connection_ref: ref,
    });
    await render();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3600000);
    });
    expect(text()).toContain('额度耗尽限制');
    expect(text()).toContain('是否已解除请刷新确认');
    expect(mocks.update).not.toHaveBeenCalled();
  });
  it('保存失败独立提示，缺失自动字段显示未知不假设none', async () => {
    mocks.list.mockResolvedValue({
      accounts: [
        {
          ...account,
          auto_restriction: {
            ...account.auto_restriction,
            reason: 'account_restricted',
            error_code: 'storage_failed',
            persisted: false,
          },
        },
        { ...account, id: 'b', auto_restriction: undefined },
      ],
      connection_ref: ref,
    });
    await render();
    expect(text()).toContain('账号受限');
    expect(text()).toContain('恢复状态保存失败');
    expect(text()).toContain('自动限制状态未知');
  });
  it('同值true明确恢复仅发送一次，另一账号限制保留', async () => {
    mocks.list
      .mockResolvedValueOnce({
        accounts: [
          account,
          {
            ...account,
            id: 'b',
            name: '账号b',
            auto_restriction: { ...account.auto_restriction, reason: 'account_restricted' },
          },
        ],
        connection_ref: ref,
      })
      .mockResolvedValue({
        accounts: [
          restored,
          {
            ...account,
            id: 'b',
            name: '账号b',
            auto_restriction: { ...account.auto_restriction, reason: 'account_restricted' },
          },
        ],
        connection_ref: ref,
      });
    await render();
    await click('解除自动限制');
    expect(mocks.update).toHaveBeenCalledTimes(1);
    expect(mocks.update).toHaveBeenCalledWith('a', { enabled: true }, ref, scope);
    expect(text()).toContain('无自动限制');
    expect(text()).toContain('账号受限');
    expect(text()).toContain('不代表模型可用');
  });
  it('停用且受限账号明确提示启用并解除，不含隐式启用按钮', async () => {
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, enabled: false }],
      connection_ref: ref,
    });
    await render();
    expect(text()).toContain('已停用');
    expect(renderer!.root.findAllByType(Button).some((b) => b.props.children === '启用')).toBe(
      false
    );
    await click('启用并解除自动限制');
    expect(mocks.update).toHaveBeenCalledWith('a', { enabled: true }, ref, scope);
  });
  it('普通编辑省略未改enabled，手动切换才显式提交', async () => {
    await render();
    await click('编辑');
    await act(async () => {
      renderer!.root
        .findAllByType(Input)
        .find((i) => i.props.label === '账号名称')!
        .props.onChange({ target: { value: '改名' } });
    });
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty('enabled');
    await click('编辑');
    await act(async () => {
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((i) => i.props.label === '启用账号')!
        .props.onChange(false);
    });
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
    expect(mocks.update.mock.calls[1][1].enabled).toBe(false);
  });
  it.each([403, 409, 499, 503])('恢复%d失败保旧限制不报成功', async (status) => {
    mocks.update.mockRejectedValue({ status });
    await render();
    await click('解除自动限制');
    expect(text()).toContain('额度耗尽限制');
    expect(text()).not.toContain('本账号已人工启用');
    expect(mocks.update).toHaveBeenCalledTimes(1);
  });
  it.each(['id', 'ref', 'reason', 'persisted'])('回执%s未确认不提前清限制', async (kind) => {
    mocks.update.mockResolvedValue({
      ...restored,
      ...(kind === 'id'
        ? { id: 'other' }
        : kind === 'ref'
          ? { connection_ref: 'b'.repeat(32) }
          : {
              auto_restriction: {
                ...restored.auto_restriction,
                ...(kind === 'reason' ? { reason: 'quota_exhausted' } : { persisted: false }),
              },
            }),
    });
    await render();
    await click('解除自动限制');
    expect(text()).toContain('额度耗尽限制');
    expect(text()).not.toContain('本账号已人工启用');
  });
  it('恢复已确认但列表失败，保实际返回状态与待刷新提示', async () => {
    mocks.list
      .mockResolvedValueOnce({ accounts: [account], connection_ref: ref })
      .mockRejectedValue({ status: 503 });
    await render();
    await click('解除自动限制');
    expect(text()).toContain('无自动限制');
    expect(text()).toContain('列表刷新待确认');
    expect(text()).toContain('本账号已人工启用');
  });
  it('切连接后恢复迟到结果不发布旧账号状态', async () => {
    const pending = deferred<typeof restored>();
    mocks.update.mockReturnValue(pending.promise);
    await render();
    await click('解除自动限制');
    mocks.list.mockResolvedValue({
      accounts: [{ ...account, id: 'new', name: '新连接' }],
      connection_ref: 'b'.repeat(32),
    });
    await act(async () => {
      renderer!.update(
        <MemoryRouter>
          <KiloAccountsPanel scope={{ ...scope, apiBase: 'https://next.invalid' }} />
        </MemoryRouter>
      );
    });
    await act(async () => {
      pending.resolve(restored);
      await flush();
    });
    expect(text()).toContain('新连接');
    expect(text()).not.toContain('本账号已人工启用');
    expect(text()).not.toContain('无自动限制');
  });
});

vi.mock('@/components/ui/DropdownMenu', () => ({
  DropdownMenu: ({
    items,
    disabled,
  }: {
    items: import('@/components/ui/DropdownMenu').DropdownMenuItem[];
    disabled?: boolean;
  }) => (
    <>
      {items.map((item) =>
        item.type === 'divider' ? null : (
          <Button
            key={item.key}
            disabled={Boolean(disabled || item.disabled)}
            onClick={item.onClick}
          >
            {item.label}
          </Button>
        )
      )}
    </>
  ),
}));
