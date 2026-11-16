import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { UpdateOperation } from '@/types/pluginImport';
import {
  SystemUpdateSection,
  type CoreUpdateDiscovery,
  type CoreUpdatePreview,
  type SystemUpdateSectionProps,
} from './SystemUpdateSection';

const discovery = (overrides: Partial<CoreUpdateDiscovery> = {}): CoreUpdateDiscovery => ({
  site_id: 'site-one',
  registered: true,
  current_version: '1.0.0',
  official_version: '9.0.0',
  site_version: '2.0.0',
  releases: [
    { id: 'release-two', version: '2.0.0', notes: '本站发布说明' },
    { id: 'release-three', version: '3.0.0', notes: '下一版本说明' },
  ],
  blockers: [],
  ...overrides,
});
const candidate = (overrides: Partial<CoreUpdatePreview> = {}): CoreUpdatePreview => ({
  id: 'candidate-pinned',
  site_id: 'site-one',
  version: '2.0.0',
  current_version: '1.0.0',
  maintenance: ['进入维护窗口', '备份当前核心与配置', '替换固定候选后重启'],
  notes: '固定候选发布说明',
  installable: true,
  blockers: [],
  ...overrides,
});
const operation = (status: UpdateOperation['status']): UpdateOperation => ({
  id: 'core-operation',
  type: 'core_update',
  target_version: '2.0.0',
  status,
  stage: status,
  active_version: status === 'completed' ? '2.0.0' : '1.0.0',
  registered: true,
  license_status: 'not_required',
  message: '',
  started_at: '2026-11-16T00:00:00Z',
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const text = (node: ReactTestInstance | string): string =>
  typeof node === 'string' ? node : node.children.map(text).join('');
let renderer: ReactTestRenderer;
let props: SystemUpdateSectionProps;
type CoreApi = SystemUpdateSectionProps['api'];
let api: { [Method in keyof CoreApi]: Mock<CoreApi[Method]> };
const button = (label: string) =>
  renderer.root.findAllByType('button').find((node) => text(node) === label)!;
const click = async (label: string) => {
  await act(async () => {
    button(label).props.onClick();
  });
};
const mount = async () => {
  await act(async () => {
    renderer = create(<SystemUpdateSection {...props} />);
  });
};
const update = async (next: Partial<SystemUpdateSectionProps>) => {
  props = { ...props, ...next };
  await act(async () => {
    renderer.update(<SystemUpdateSection {...props} />);
  });
};
const select = async (id = 'release-two') => {
  await act(async () => {
    renderer.root
      .findByProps({ 'aria-label': '本站发布版本' })
      .props.onChange({ target: { value: id } });
  });
};
const confirm = async (checked = true) => {
  await act(async () => {
    renderer.root
      .findByProps({ 'aria-label': '确认维护备份影响' })
      .props.onChange({ target: { checked } });
  });
};
const preview = async () => {
  await select();
  await click('生成更新预览');
};
const install = async () => {
  await preview();
  await confirm();
  await click('确认更新此候选');
};
const expectNoSuccess = () =>
  expect(renderer.root.findAllByProps({ 'data-core-update-state': 'success' })).toHaveLength(0);

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  api = {
    discover: vi.fn().mockResolvedValue(discovery()),
    preview: vi.fn().mockResolvedValue(candidate()),
    install: vi.fn().mockResolvedValue(operation('queued')),
    getOperation: vi.fn().mockResolvedValue(operation('installing')),
  };
  props = { connectionScope: 'scope-one', api };
});
afterEach(async () => {
  if (renderer)
    await act(async () => {
      renderer.unmount();
    });
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('核心更新组件边界', () => {
  it('加载自动发现，官方仅展示，点击刷新重新发现', async () => {
    await mount();
    expect(api.discover).toHaveBeenCalledOnce();
    expect(text(renderer.root)).toContain('9.0.0');
    expect(text(renderer.root)).toContain('安装仅接受本站发布版本');
    const options = renderer.root.findAllByType('option').map((node) => node.props.value);
    expect(options).toEqual(['', 'release-two', 'release-three']);
    expect(button('生成更新预览').props.disabled).toBe(true);
    await click('刷新版本与任务');
    expect(api.discover).toHaveBeenCalledTimes(2);
    expect(api.install).not.toHaveBeenCalled();
  });

  it('本站发布选择生成固定候选，明确勾选维护备份影响后提交候选编号', async () => {
    await mount();
    await preview();
    expect(api.preview).toHaveBeenCalledExactlyOnceWith('release-two');
    expect(text(renderer.root)).toContain('备份当前核心与配置');
    expect(text(renderer.root)).toContain('固定候选发布说明');
    expect(button('确认更新此候选').props.disabled).toBe(true);
    await click('确认更新此候选');
    expect(api.install).not.toHaveBeenCalled();
    await confirm();
    expect(button('确认更新此候选').props.disabled).toBe(false);
    await click('确认更新此候选');
    expect(api.install).toHaveBeenCalledExactlyOnceWith('candidate-pinned');
    expect(api.getOperation).toHaveBeenCalledExactlyOnceWith('core-operation');
    expectNoSuccess();
  });

  it.each([
    { registered: false, blockers: [] },
    { registered: true, blockers: ['存在后台维护阻断'] },
  ])('注册或发现阻断时停止预览：%o', async (state) => {
    api.discover.mockResolvedValue(discovery(state));
    await mount();
    await select();
    expect(button('生成更新预览').props.disabled).toBe(true);
    await click('生成更新预览');
    expect(api.preview).not.toHaveBeenCalled();
  });

  it.each([
    { installable: false, blockers: [] },
    { installable: true, blockers: ['候选有阻断项'] },
  ])('候选明确不可安装或有阻断项时拒绝提交：%o', async (state) => {
    api.preview.mockResolvedValue(candidate(state));
    await mount();
    await preview();
    await confirm();
    expect(button('确认更新此候选').props.disabled).toBe(true);
    await click('确认更新此候选');
    expect(api.install).not.toHaveBeenCalled();
  });

  it('选择另一本站发布后旧候选与确认均失效', async () => {
    await mount();
    await preview();
    await confirm();
    await select('release-three');
    expect(renderer.root.findAllByProps({ 'aria-label': '固定更新候选' })).toHaveLength(0);
    api.preview.mockResolvedValue(candidate({ id: 'candidate-three', version: '3.0.0' }));
    await click('生成更新预览');
    expect(api.preview).toHaveBeenLastCalledWith('release-three');
    expect(button('确认更新此候选').props.disabled).toBe(true);
    expect(api.install).not.toHaveBeenCalled();
  });

  it('连续确认只安装一次，受理响应声称完成仍需同编号实时核验', async () => {
    const accepted = deferred<UpdateOperation>();
    const queried = deferred<UpdateOperation>();
    api.install.mockReturnValue(accepted.promise);
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    await preview();
    await confirm();
    const submit = button('确认更新此候选');
    await act(async () => {
      submit.props.onClick();
      submit.props.onClick();
    });
    expect(api.install).toHaveBeenCalledExactlyOnceWith('candidate-pinned');
    await act(async () => {
      accepted.resolve(operation('completed'));
    });
    expectNoSuccess();
    expect(text(renderer.root)).toContain('正在核验核心更新结果');
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expect(renderer.root.findByProps({ 'data-core-update-state': 'success' })).toBeDefined();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(api.getOperation).toHaveBeenCalledOnce();
  });

  it('discover.active_operation 恢复原任务轮询，不重新预览或安装', async () => {
    api.discover.mockResolvedValue(discovery({ active_operation: operation('installing') }));
    api.getOperation
      .mockResolvedValueOnce(operation('installing'))
      .mockResolvedValueOnce(operation('completed'));
    await mount();
    expectNoSuccess();
    expect(button('刷新版本与任务').props.disabled).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(api.getOperation.mock.calls).toEqual([['core-operation'], ['core-operation']]);
    expect(renderer.root.findByProps({ 'data-core-update-state': 'success' })).toBeDefined();
    expect(api.preview).not.toHaveBeenCalled();
    expect(api.install).not.toHaveBeenCalled();
  });

  it('发现快照即使声称完成也等实时查询，不提前标绿', async () => {
    const queried = deferred<UpdateOperation>();
    api.discover.mockResolvedValue(discovery({ active_operation: operation('completed') }));
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    expectNoSuccess();
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expect(renderer.root.findByProps({ 'data-core-update-state': 'success' })).toBeDefined();
  });

  it.each(['recovery_required', 'awaiting_restart'] as const)(
    '%s 持续查询且不标绿',
    async (status) => {
      api.getOperation.mockResolvedValue(operation(status));
      await mount();
      await install();
      expect(renderer.root.findByProps({ 'data-core-update-state': 'attention' })).toBeDefined();
      expectNoSuccess();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3000);
      });
      expect(api.getOperation).toHaveBeenCalledTimes(3);
    }
  );

  it.each(['failed', 'license_unverified', 'license_error', 'completed_disabled'] as const)(
    '%s 结束查询但不声称核心运行成功',
    async (status) => {
      api.getOperation.mockResolvedValue(operation(status));
      await mount();
      await install();
      expectNoSuccess();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(6000);
      });
      expect(api.getOperation).toHaveBeenCalledOnce();
    }
  );

  it('状态查询失败重试原编号，不再次安装', async () => {
    api.getOperation
      .mockRejectedValueOnce(new Error('连接中断'))
      .mockResolvedValueOnce(operation('completed'));
    await mount();
    await install();
    expectNoSuccess();
    expect(text(renderer.root.findByProps({ role: 'alert' }))).toContain('连接中断');
    await click('重试状态查询');
    expect(api.install).toHaveBeenCalledOnce();
    expect(api.getOperation.mock.calls).toEqual([['core-operation'], ['core-operation']]);
    expect(renderer.root.findByProps({ 'data-core-update-state': 'success' })).toBeDefined();
  });

  it('错误任务编号不接受完成结果', async () => {
    api.getOperation.mockResolvedValue({ ...operation('completed'), id: 'another-operation' });
    await mount();
    await install();
    expectNoSuccess();
    expect(text(renderer.root.findByProps({ role: 'alert' }))).toContain('当前任务不一致');
  });

  it('disabled 时不发请求，重连自动发现任务但不凭连接恢复标绿', async () => {
    props.disabled = true;
    await mount();
    expect(api.discover).not.toHaveBeenCalled();
    await update({ disabled: false });
    await install();
    const late = deferred<UpdateOperation>();
    api.getOperation.mockReturnValue(late.promise);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    await update({ disabled: true });
    expectNoSuccess();
    await act(async () => {
      late.resolve(operation('completed'));
    });
    expectNoSuccess();
    const reconnectQuery = deferred<UpdateOperation>();
    api.discover.mockResolvedValue(discovery({ active_operation: operation('completed') }));
    api.getOperation.mockReturnValue(reconnectQuery.promise);
    await update({ disabled: false });
    expectNoSuccess();
    expect(text(renderer.root)).toContain('正在核验核心更新结果');
    await act(async () => {
      reconnectQuery.resolve(operation('installing'));
    });
    expectNoSuccess();
    expect(api.install).toHaveBeenCalledOnce();
  });

  it('切连接丢弃旧发现与预览迟到响应', async () => {
    const oldDiscovery = deferred<CoreUpdateDiscovery>();
    api.discover.mockReturnValueOnce(oldDiscovery.promise).mockResolvedValue(discovery());
    await mount();
    await update({ connectionScope: 'scope-two' });
    await act(async () => {
      oldDiscovery.resolve(discovery({ site_id: 'old-site' }));
    });
    expect(text(renderer.root)).not.toContain('old-site');
    const oldPreview = deferred<CoreUpdatePreview>();
    api.preview.mockReturnValue(oldPreview.promise);
    await preview();
    await update({ connectionScope: 'scope-three' });
    await act(async () => {
      oldPreview.resolve(candidate());
    });
    expect(renderer.root.findAllByProps({ 'aria-label': '固定更新候选' })).toHaveLength(0);
    expect(api.install).not.toHaveBeenCalled();
  });

  it('切连接丢弃旧操作完成，不污染新连接', async () => {
    const queried = deferred<UpdateOperation>();
    api.getOperation.mockReturnValue(queried.promise);
    await mount();
    await install();
    await update({ connectionScope: 'scope-two' });
    await act(async () => {
      queried.resolve(operation('completed'));
    });
    expectNoSuccess();
    expect(text(renderer.root)).not.toContain('core-operation');
    expect(api.discover).toHaveBeenCalledTimes(2);
  });

  it('安装受理未知时通过重新发现恢复，不提供重复提交按钮', async () => {
    api.install.mockRejectedValue(new Error('连接中断'));
    await mount();
    await install();
    expectNoSuccess();
    expect(
      renderer.root.findAllByType('button').filter((node) => text(node) === '确认更新此候选')
    ).toHaveLength(0);
    api.discover.mockResolvedValue(discovery({ active_operation: operation('installing') }));
    await click('刷新版本与任务');
    expect(api.getOperation).toHaveBeenCalledExactlyOnceWith('core-operation');
    expect(api.install).toHaveBeenCalledOnce();
  });
});
