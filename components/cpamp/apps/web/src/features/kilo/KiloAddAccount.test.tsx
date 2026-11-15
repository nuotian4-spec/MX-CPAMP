import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { KiloAddAccountEditor } from './KiloAddAccount';
import { KiloDeviceForm } from './KiloDeviceForm';
import { KiloDeviceAuth } from './KiloDeviceAuth';
import { KiloQuotaSourceFields } from './KiloQuotaSourceFields';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
const navigate = vi.hoisted(() => vi.fn());
vi.mock('react-router-dom', () => ({ useNavigate: () => navigate }));
vi.mock('@/components/ui/Select', () => ({ Select: () => null }));
const accounts = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), list: vi.fn() }));
const proxies = vi.hoisted(() => ({ list: vi.fn(), egress: vi.fn() }));
const quota = vi.hoisted(() => ({ sources: vi.fn(), get: vi.fn(), put: vi.fn(), query: vi.fn() }));
const device = vi.hoisted(() => ({
  start: vi.fn(),
  poll: vi.fn(),
  save: vi.fn(),
  cancel: vi.fn(),
}));
vi.mock('@/services/api/kilo', () => ({ kiloApi: accounts }));
vi.mock('@/services/api/kiloProxies', () => ({ kiloProxiesApi: proxies }));
vi.mock('@/services/api/kiloQuota', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloQuota')>()),
  kiloQuotaApi: quota,
}));
vi.mock('@/services/api/kiloDevice', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloDevice')>()),
  kiloDeviceApi: device,
}));
const scope = { apiBase: 'http://127.0.0.1:13940', managementKey: 'SYNTHETIC' };
const ref = 'connection-a',
  id = 'synthetic-saved';
let renderer: ReactTestRenderer;
const text = () => JSON.stringify(renderer.toJSON());
beforeEach(() => {
  vi.resetAllMocks();
  vi.useFakeTimers();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  proxies.list.mockResolvedValue({
    revision: 4,
    connection_ref: ref,
    proxies: [
      { id: 'shared', name: '合成共享入口', account_id: '', observed_exit_ip: '203.0.113.10' },
      { id: 'owned', name: '已独占', account_id: 'other' },
    ],
    accounts: [],
    recycle: [],
  });
  proxies.egress.mockImplementation(async (_id, input) => ({
    revision: 5,
    connection_ref: ref,
    proxies: [],
    accounts: [{ account_id: id, ...input }],
  }));
  accounts.create.mockResolvedValue({ id, enabled: false, connection_ref: ref });
  accounts.list.mockResolvedValue({ connection_ref: ref, accounts: [{ id, enabled: false }] });
  accounts.update.mockResolvedValue({ id, enabled: true, connection_ref: ref });
  quota.sources.mockResolvedValue({
    connection_ref: ref,
    sources: [
      {
        credential_id: 'source-a',
        label: '合成 Codex',
        chatgpt_account_id: 'outer-a',
        status: 'ready',
      },
    ],
  });
  quota.get.mockResolvedValue({
    account_id: id,
    connection_ref: ref,
    binding_revision: 2,
    source: { kind: 'none', status: 'none' },
  });
  quota.put.mockResolvedValue({
    account_id: id,
    connection_ref: ref,
    binding_revision: 3,
    source: { kind: 'cpa_codex', credential_id: 'source-a', status: 'ready' },
  });
  device.start.mockResolvedValue({
    id: 'device-session',
    user_code: 'SYNTHETIC',
    verification_url: 'https://example.invalid',
    expires_in: 600,
    interval: 3,
    connection_ref: ref,
    authorization_egress: { mode: 'pool', proxy_id: 'shared' },
  });
  device.poll.mockResolvedValue({ status: 'pending', connection_ref: ref });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
const render = async () => {
  await act(async () => {
    renderer = create(<KiloAddAccountEditor scope={scope} connectionRef={ref} />);
  });
};
const input = async (label: string, value: string) => {
  await act(async () => {
    renderer.root
      .findAllByType(Input)
      .find((x) => x.props.label === label)!
      .props.onChange({ target: { value } });
  });
};
const select = async (label: string, value: string) => {
  await act(async () => {
    renderer.root
      .findAllByType(Select)
      .find((x) => x.props.ariaLabel === label)!
      .props.onChange(value);
  });
};
const submit = async () => {
  await act(async () => {
    await renderer.root.findByType('form').props.onSubmit({ preventDefault() {} });
  });
};
const pickSource = async () => {
  await act(async () => {
    const fields = renderer.root.findByType(KiloQuotaSourceFields);
    fields.props.onChange({ ...fields.props.value, mode: 'cpa_codex', selected: 'source-a' });
  });
};
it('同页呈现四区和两种方式，来源只读不查额度，授权排除他人独占', async () => {
  await render();
  for (const heading of ['账号信息', '关联 Codex 额度', '保存后的推理出口', '本次授权'])
    expect(text()).toContain(heading);
  expect(text()).not.toContain('下一步');
  expect(renderer.root.findAllByType('form')).toHaveLength(1);
  expect(quota.sources).toHaveBeenCalledTimes(1);
  expect(quota.query).not.toHaveBeenCalled();
  const auth = renderer.root.findAllByType(Select).find((x) => x.props.ariaLabel === '授权出口')!;
  expect(auth.props.options.map((x: { value: string }) => x.value)).toEqual([
    '',
    'shared',
    'direct',
  ]);
  await select('授权方式', 'import');
  expect(renderer.root.findAllByType(Input).some((x) => x.props.label === 'API Key')).toBe(true);
});
it('设备授权开始后配置保留并锁定，已保存ID和关联失败原页呈现', async () => {
  await render();
  await input('账号名称', '合成设备');
  await pickSource();
  await select('授权出口', 'shared');
  await submit();
  expect(renderer.root.findByType(KiloDeviceForm).props.locked).toBe(true);
  expect(text()).toContain('等待授权');
  quota.put.mockRejectedValueOnce({ status: 409 });
  await act(async () =>
    renderer.root.findByType(KiloDeviceAuth).props.onSaved(id, {
      id,
      connection_ref: ref,
      enabled: true,
      credential_saved: true,
      egress_ready: true,
      egress_mode: 'pool',
      egress_status: 'available',
      egress_reason: '',
    })
  );
  expect(text()).toContain('凭证已保存');
  expect(text()).toContain('额度关联待完成');
  expect(text()).not.toContain('账号列表待刷新');
  expect(accounts.create).not.toHaveBeenCalled();
  expect(proxies.egress).not.toHaveBeenCalled();
  expect(navigate).not.toHaveBeenCalled();
});
it('API Key保存完整链后停留原页并清空密钥', async () => {
  await render();
  await input('账号名称', '合成导入');
  await select('授权方式', 'import');
  await input('API Key', 'SYNTHETIC_KEY');
  await select('出口策略', 'direct');
  await submit();
  expect(accounts.create).toHaveBeenCalledTimes(1);
  expect(accounts.create.mock.calls[0][0].enabled).toBe(false);
  expect(accounts.update).toHaveBeenCalledTimes(1);
  expect(text()).toContain('保存完成');
  expect(navigate).not.toHaveBeenCalled();
  expect(
    renderer.root.findAllByType(Input).find((x) => x.props.label === 'API Key')!.props.value
  ).toBe('');
});
it('快速双击导入只创建一次，已保存后的提交保持锁定', async () => {
  let resolve!: (value: { id: string; enabled: boolean; connection_ref: string }) => void;
  accounts.create.mockReturnValueOnce(
    new Promise((done) => {
      resolve = done;
    })
  );
  await render();
  await input('账号名称', '双击合成导入');
  await select('授权方式', 'import');
  await input('API Key', 'SYNTHETIC_KEY');
  let pending!: Promise<void>;
  await act(async () => {
    const form = renderer.root.findByType('form');
    pending = form.props.onSubmit({ preventDefault() {} });
    await form.props.onSubmit({ preventDefault() {} });
  });
  expect(accounts.create).toHaveBeenCalledTimes(1);
  await act(async () => {
    resolve({ id, enabled: false, connection_ref: ref });
    await pending;
  });
  await submit();
  expect(accounts.create).toHaveBeenCalledTimes(1);
});
it('API出口失败保留ID及只读恢复入口，连接切换不丢已知结果', async () => {
  proxies.egress.mockRejectedValueOnce({ status: 409 });
  await render();
  await input('账号名称', '合成导入');
  await select('授权方式', 'import');
  await input('API Key', 'SYNTHETIC_KEY');
  await submit();
  expect(text()).toContain(id);
  expect(text()).toContain('正式出口待完成');
  expect(text()).toContain('读取已保存状态');
  await act(async () =>
    renderer.update(<KiloAddAccountEditor scope={scope} connectionRef="other-connection" />)
  );
  expect(text()).toContain(id);
  expect(text()).toContain('连接已变化');
  expect(accounts.create).toHaveBeenCalledTimes(1);
});
it.each(['scope', 'reference'] as const)(
  '未提交合成Key在%s切换后清空且不参与新连接创建',
  async (change) => {
    await render();
    await input('账号名称', '连接隔离');
    await select('授权方式', 'import');
    await input('API Key', 'OLD_SYNTHETIC_KEY');
    const nextScope = change === 'scope' ? { ...scope, apiBase: 'http://127.0.0.1:13941' } : scope;
    const nextRef = change === 'reference' ? 'connection-b' : ref;
    proxies.list.mockResolvedValue({
      revision: 8,
      connection_ref: nextRef,
      proxies: [],
      accounts: [],
      recycle: [],
    });
    quota.sources.mockResolvedValue({ connection_ref: nextRef, sources: [] });
    await act(async () =>
      renderer.update(<KiloAddAccountEditor scope={nextScope} connectionRef={nextRef} />)
    );
    expect(
      renderer.root.findAllByType(Input).find((node) => node.props.label === 'API Key')!.props.value
    ).toBe('');
    await submit();
    expect(accounts.create).not.toHaveBeenCalled();
  }
);
