import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloAutoAssignmentForm, KiloIPGroups, KiloAllocationStatus } from './KiloAutoEgress';
import { KiloEgressFields } from './KiloEgressFields';
import { KiloEgressSummary } from './KiloEgressSummary';
import { KiloProxyForm } from './KiloProxyForms';
import { KiloDeviceForm } from './KiloDeviceForm';
import type { KiloProxyState, KiloProxy } from '@/services/api/kiloProxies';
const mocks = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: mocks,
}));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const scope = { apiBase: 'https://manager.invalid', managementKey: 'SYNTHETIC' };
const snapshot: KiloProxyState = {
  revision: 3,
  runtime_revision: 4,
  managed: true,
  proxy_limit: 10,
  connection_ref: 'a'.repeat(32),
  auto_assignment_enabled: false,
  max_unique_accounts: 0,
  auto_error: null,
  ip_groups: [],
  proxies: [],
  accounts: [],
  recycle: [],
};
const proxy: KiloProxy = {
  id: 'p1',
  name: '合成代理',
  order: 1,
  enabled: true,
  account_id: '',
  scheme: 'http',
  host: 'proxy.invalid',
  port: '8080',
  has_auth: false,
  healthy: true,
  active: 0,
  owner: '',
  observed_exit_ip: '203.0.113.10',
  observation_status: 'ok',
  fixed_exit_ip: '',
};
describe('自动独占出口管理', () => {
  let renderer: ReactTestRenderer | undefined;
  const onSave = vi.fn(),
    onClose = vi.fn();
  beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    onSave.mockResolvedValue(undefined);
    mocks.list.mockResolvedValue(snapshot);
  });
  afterEach(async () => {
    await act(async () => renderer?.unmount());
    renderer = undefined;
    vi.unstubAllGlobals();
  });
  const text = () => JSON.stringify(renderer!.toJSON());
  const renderForm = async (state = snapshot) => {
    await act(async () => {
      renderer = create(
        <KiloAutoAssignmentForm snapshot={state} busy={false} onSave={onSave} onClose={onClose} />
      );
    });
  };
  const submit = async () => {
    await act(async () => {
      await renderer!.root.findByType('form').props.onSubmit({ preventDefault() {} });
    });
  };
  it('默认关且N未配置不写作无限，累计与并发明确分开', async () => {
    await renderForm();
    expect(renderer!.root.findByType(ToggleSwitch).props.checked).toBe(false);
    expect(
      renderer!.root
        .findAllByType(Input)
        .find((input) => input.props.label === '每IP累计不同账号上限N')!.props.value
    ).toBe('');
    expect(text()).toContain('不是请求并发容量');
    expect(text()).toContain('不自动恢复已退休IP');
    await act(async () => renderer!.root.findByType(ToggleSwitch).props.onChange(true));
    await submit();
    expect(onSave).not.toHaveBeenCalled();
    expect(text()).toContain('不表示无限');
  });
  it.each(['0', '-1', '1.5', '9007199254740992'])('N=%s拒绝', async (value) => {
    await renderForm();
    await act(async () => {
      renderer!.root.findByType(ToggleSwitch).props.onChange(true);
      renderer!.root
        .findAllByType(Input)
        .find((input) => input.props.label === '每IP累计不同账号上限N')!
        .props.onChange({ target: { value } });
    });
    await submit();
    expect(onSave).not.toHaveBeenCalled();
  });
  it('累计N与IP并发容量分别提交，保留固定模式代理限额', async () => {
    await renderForm({ ...snapshot, ip_concurrency_limit: 30, ip_entry_limit: 7 });
    await act(async () => {
      renderer!.root.findByType(ToggleSwitch).props.onChange(true);
      renderer!.root
        .findAllByType(Input)
        .find((input) => input.props.label === '每IP累计不同账号上限N')!
        .props.onChange({ target: { value: '3' } });
    });
    await submit();
    expect(onSave).toHaveBeenCalledWith({
      auto_assignment_enabled: true,
      max_unique_accounts: 3,
      ip_concurrency_limit: 30,
      ip_entry_limit: 7,
    });
  });
  it('未知配置禁止提交，不以关闭或0补值', async () => {
    await renderForm({
      ...snapshot,
      auto_assignment_enabled: undefined,
      max_unique_accounts: undefined,
    });
    expect(text()).toContain('状态未知');
    expect(
      renderer!.root.findAllByType(Button).find((b) => b.props.type === 'submit')!.props.disabled
    ).toBe(true);
    await submit();
    expect(onSave).not.toHaveBeenCalled();
  });
  it('当前owner与历史ID独立显示，退休保留显式恢复说明', async () => {
    await act(async () => {
      renderer = create(
        <MemoryRouter>
          <KiloIPGroups
            recycled
            snapshot={{
              ...snapshot,
              auto_assignment_enabled: true,
              max_unique_accounts: 3,
              ip_groups: [
                {
                  ip: '203.0.113.10',
                  owner_account_id: 'current',
                  account_ids: ['historical', 'current'],
                  unique_accounts: 2,
                  max_unique_accounts: 3,
                  draining: false,
                  retired: true,
                  reason: 'unique_account_limit',
                  state: 'retired',
                  proxy_ids: ['p1'],
                },
              ],
            }}
            accountLabel={(id) => id}
          />
        </MemoryRouter>
      );
    });
    const row = renderer!.root.findAllByType('tr')[1];
    const owner = row.findAllByType('td')[1];
    expect(JSON.stringify(owner.findByType('a').props.children)).toContain('current');
    expect(owner.findByType('a').props.href).toBe('/kilo-accounts?account=current');
    await act(async () =>
      renderer!.root
        .findAllByType(Button)
        .find((button) => button.props.children === '详情')!
        .props.onClick()
    );
    expect(text()).toContain('累计分配历史');
    expect(text()).toContain('historical');
    expect(text()).toContain('已退休');
  });
  it('auto模式提交空手工绑定且不显示主偏好', async () => {
    await act(async () => {
      renderer = create(
        <KiloEgressFields
          value={{ mode: 'pool', exclusive_proxy_ids: ['p1'], primary_proxy_id: 'p1' }}
          onChange={onSave}
          snapshot={snapshot}
          disabled={false}
        />
      );
    });
    await act(async () =>
      renderer!.root
        .findAllByType(Select)
        .find((s) => s.props.ariaLabel === '出口策略')!
        .props.onChange('auto')
    );
    expect(onSave).toHaveBeenCalledWith({
      capacity_mode: 'ip',
      allocation_mode: 'auto',
      max_ips: 1,
      specified_ips: [],
      mode: 'auto',
      candidate_proxy_ids: null,
      exclusive_proxy_ids: [],
      primary_proxy_id: '',
    });
    await act(async () =>
      renderer!.update(
        <KiloEgressFields
          value={onSave.mock.calls[0][0]}
          onChange={onSave}
          snapshot={snapshot}
          disabled={false}
        />
      )
    );
    expect(
      renderer!.root.findAllByType(Select).some((item) => item.props.ariaLabel === '主偏好')
    ).toBe(false);
    expect(text()).toContain('凭证保存与出口配齐分别确认');
  });
  it('账号卡片缺IP显示等待，未知保持未知', async () => {
    await act(async () => {
      renderer = create(
        <KiloEgressSummary
          accountID="a"
          snapshot={{
            ...snapshot,
            accounts: [
              {
                account_id: 'a',
                mode: 'auto',
                primary_proxy_id: '',
                assigned_ip: '',
                allocation_state: 'waiting',
              },
            ],
          }}
        />
      );
    });
    expect(text()).toContain('等待空闲固定IP');
    expect(text()).not.toContain('已分配独占IP');
    await act(async () => renderer!.update(<KiloAllocationStatus />));
    expect(text()).toContain('状态未知');
  });
  it('展开正式出口显示每IP真实占用和容量，缺容量仍为未知', async () => {
    const state: KiloProxyState = {
      ...snapshot,
      ip_concurrency_limit: 37,
      accounts: [
        { account_id: 'a', mode: 'auto', primary_proxy_id: '', assigned_ips: ['203.0.113.10'] },
      ],
      ip_groups: [
        {
          ip: '203.0.113.10',
          owner_account_id: 'a',
          account_ids: ['a'],
          draining: false,
          retired: false,
          reason: '',
          unique_accounts: 1,
          max_unique_accounts: 10,
          state: 'assigned',
          proxy_ids: ['p1'],
          inference_active: 7,
          management_active: 2,
          capacity: 13,
        },
      ],
    };
    await act(async () => {
      renderer = create(<KiloEgressSummary accountID="a" snapshot={state} />);
    });
    const item = () => renderer!.root.findByType('li').findByType('small').children.join('');
    expect(item()).toContain('7');
    expect(item()).toContain('2');
    expect(item()).toContain('13');
    expect(item()).not.toContain('37');
    await act(async () =>
      renderer!.update(
        <KiloEgressSummary
          accountID="a"
          snapshot={{ ...state, ip_groups: [{ ...state.ip_groups![0], capacity: undefined }] }}
        />
      )
    );
    expect(item()).toContain('未知');
  });
  it('固定IP必须显式确认，替换URL后重新核验', async () => {
    await act(async () => {
      renderer = create(
        <KiloProxyForm proxy={proxy} accounts={[]} busy={false} onSave={onSave} onClose={onClose} />
      );
    });
    await submit();
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('fixed_exit_ip');
    await act(async () =>
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((s) => s.props.label.startsWith('明确确认'))!
        .props.onChange(true)
    );
    await submit();
    expect(onSave.mock.calls[1][0].fixed_exit_ip).toBe('203.0.113.10');
    await act(async () =>
      renderer!.root
        .findAllByType(Input)
        .find((i) => i.props.type === 'password')!
        .props.onChange({ target: { value: 'http://proxy2.invalid:8080' } })
    );
    await submit();
    expect(onSave.mock.calls[2][0]).not.toHaveProperty('fixed_exit_ip');
  });
  it('固定IP自动池条目不进入手工候选与主偏好，包括同账号归属', async () => {
    await act(async () => {
      renderer = create(
        <KiloEgressFields
          value={{
            mode: 'pool',
            candidate_proxy_ids: [],
            exclusive_proxy_ids: [],
            primary_proxy_id: '',
          }}
          onChange={onSave}
          accountID="a"
          snapshot={{
            ...snapshot,
            proxies: [
              { ...proxy, fixed_exit_ip: '203.0.113.10', account_id: 'a' },
              { ...proxy, id: 'manual', name: '手工代理' },
            ],
          }}
          disabled={false}
        />
      );
    });
    const preference = renderer!.root
      .findAllByType(Select)
      .find((item) => item.props.ariaLabel === '主偏好')!;
    expect(preference.props.options.some((item: { value: string }) => item.value === 'p1')).toBe(
      false
    );
    expect(text()).not.toContain('合成代理');
    expect(text()).toContain('手工代理');
    expect(text()).toContain('属于自动独占池');
  });
  it('手工归属必须先保存解除，不能同次确认固定IP', async () => {
    await act(async () => {
      renderer = create(
        <KiloProxyForm
          proxy={{ ...proxy, account_id: 'a' }}
          accounts={[]}
          busy={false}
          onSave={onSave}
          onClose={onClose}
        />
      );
    });
    await act(async () => renderer!.root.findByType(Select).props.onChange(''));
    expect(
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label.startsWith('明确确认'))!.props.disabled
    ).toBe(true);
    expect(text()).toContain('同次提交不合并解除与确认');
    await submit();
    expect(onSave.mock.calls[0][0].account_id).toBe('');
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('fixed_exit_ip');
  });
  it('已存在手工出口引用时固定确认保持禁用，自动池代理禁止选手工归属', async () => {
    await act(async () => {
      renderer = create(
        <KiloProxyForm
          proxy={proxy}
          accounts={[]}
          hasManualReferences
          busy={false}
          onSave={onSave}
          onClose={onClose}
        />
      );
    });
    expect(
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label.startsWith('明确确认'))!.props.disabled
    ).toBe(true);
    await act(async () =>
      renderer!.update(
        <KiloProxyForm
          proxy={{ ...proxy, fixed_exit_ip: '203.0.113.10' }}
          accounts={[]}
          busy={false}
          onSave={onSave}
          onClose={onClose}
        />
      )
    );
    expect(renderer!.root.findByType(Select).props.disabled).toBe(true);
  });
  it('新增手工归属清除固定确认，避免同次组合提交', async () => {
    await act(async () => {
      renderer = create(
        <KiloProxyForm proxy={proxy} accounts={[]} busy={false} onSave={onSave} onClose={onClose} />
      );
    });
    await act(async () =>
      renderer!.root
        .findAllByType(ToggleSwitch)
        .find((item) => item.props.label.startsWith('明确确认'))!
        .props.onChange(true)
    );
    await act(async () => renderer!.root.findByType(Select).props.onChange('a'));
    const fixed = renderer!.root
      .findAllByType(ToggleSwitch)
      .find((item) => item.props.label.startsWith('明确确认'))!;
    expect(fixed.props.checked).toBe(false);
    expect(fixed.props.disabled).toBe(true);
    await submit();
    expect(onSave.mock.calls[0][0].account_id).toBe('a');
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('fixed_exit_ip');
  });
  it('auto开启时设备推理默认auto，授权仍显式独立选择', async () => {
    mocks.list.mockResolvedValue({
      ...snapshot,
      auto_assignment_enabled: true,
      max_unique_accounts: 3,
    });
    await act(async () => {
      renderer = create(
        <KiloDeviceForm
          busy={false}
          scope={scope}
          connectionRef={snapshot.connection_ref}
          onStart={onSave}
        />
      );
    });
    await act(async () =>
      renderer!.root
        .findAllByType(Input)
        .find((input) => input.props.label === '账号名称')!
        .props.onChange({ target: { value: '合成账号' } })
    );
    await submit();
    expect(
      renderer!.root.findAllByType(Select).find((item) => item.props.ariaLabel === '出口策略')!
        .props.value
    ).toBe('auto');
    expect(onSave).not.toHaveBeenCalled();
    await submit();
    await act(async () =>
      renderer!.root
        .findAllByType(Select)
        .find((item) => item.props.ariaLabel === '授权出口')!
        .props.onChange('direct')
    );
    await submit();
    expect(onSave.mock.calls[0][0].inference_egress).toEqual({
      capacity_mode: 'ip',
      allocation_mode: 'auto',
      max_ips: 1,
      specified_ips: [],
      mode: 'auto',
      candidate_proxy_ids: null,
      exclusive_proxy_ids: [],
      primary_proxy_id: '',
      expected_revision: 3,
    });
    expect(onSave.mock.calls[0][0].authorization_egress.mode).toBe('direct');
    await act(async () =>
      renderer!.root
        .findAllByType(Select)
        .find((s) => s.props.ariaLabel === '出口策略')!
        .props.onChange('pool')
    );
    await submit();
    await submit();
    expect(onSave.mock.calls[1][0].inference_egress.mode).toBe('pool');
  });
});

vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({ children }: { children: import('react').ReactNode }) => (
    <div role="dialog">{children}</div>
  ),
}));
