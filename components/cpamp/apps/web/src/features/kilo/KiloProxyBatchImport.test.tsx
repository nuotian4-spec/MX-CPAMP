import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PropsWithChildren, ReactNode } from 'react';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { KiloProxyBatchImport } from './KiloProxyBatchImport';
import type { KiloProxyState } from '@/services/api/kiloProxies';
const api = vi.hoisted(() => ({ create: vi.fn(), list: vi.fn(), preview: vi.fn() }));
vi.mock('@/services/api/kiloProxies', async (original) => ({
  ...(await original<typeof import('@/services/api/kiloProxies')>()),
  kiloProxiesApi: api,
}));
vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({ children, footer }: PropsWithChildren<{ footer: ReactNode }>) => (
    <div role="dialog">
      {children}
      {footer}
    </div>
  ),
}));
const scope = { apiBase: 'http://127.0.0.1:10740', managementKey: 'SYNTHETIC' };
const snapshot = {
  revision: 3,
  runtime_revision: 1,
  connection_ref: 'local',
  proxies: [],
  recycle: [],
  accounts: [],
  proxy_limit: 10,
  managed: true,
} as KiloProxyState;
let renderer: ReactTestRenderer;
const acquire = vi.fn(),
  release = vi.fn(),
  onClose = vi.fn();
const button = (label: string) =>
  renderer.root.findAllByType(Button).find((b) => b.props.children === label)!;
const render = async (nextScope = scope) => {
  await act(async () => {
    const element = (
      <KiloProxyBatchImport
        scope={nextScope}
        snapshot={snapshot}
        acquire={acquire}
        release={release}
        onClose={onClose}
        blocked={false}
      />
    );
    if (renderer) renderer.update(element);
    else renderer = create(element);
  });
};
const input = async (value: string) => {
  await act(async () => renderer.root.findByType('textarea').props.onChange({ target: { value } }));
};
const click = async (label: string) => {
  await act(async () => button(label).props.onClick());
};
const prepare = async () => {
  await input('http://USER:SECRET@192.0.2.1:8080\nhttp://192.0.2.2:8080');
  await click('逐行预览');
  await act(async () =>
    renderer.root.findByType('input').props.onChange({ target: { checked: true } })
  );
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  acquire.mockReturnValue(true);
  api.list.mockResolvedValue(snapshot);
  api.preview.mockImplementation(async (url: string) => ({
    status: 'ready',
    reason: '',
    observed_exit_ip: url.includes('192.0.2.1') ? '203.0.113.10' : '203.0.113.11',
    observed_at: '2026-11-15T00:00:00Z',
  }));
  api.create.mockImplementation(async (_input, old: KiloProxyState) => ({
    ...old,
    revision: old.revision + 1,
    proxies: [...old.proxies, { id: 'p' + old.revision }],
  }));
});
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  renderer = undefined as unknown as ReactTestRenderer;
  vi.unstubAllGlobals();
});
describe('批量导入抽屉', () => {
  it('修改原文废弃旧预览及确认，不保存认证明文', async () => {
    await render();
    await prepare();
    expect(button('仅导入有效行').props.disabled).toBe(false);
    await input('http://changed:8080');
    expect(button('仅导入有效行').props.disabled).toBe(true);
    expect(renderer.root.findAllByType('li')).toHaveLength(0);
    expect(api.create).not.toHaveBeenCalled();
  });
  it('提交清空原文、双击只运行一次，逐行最新版本保存', async () => {
    await render();
    await prepare();
    const submit = button('仅导入有效行').props.onClick;
    await act(async () => {
      submit();
      submit();
    });
    expect(acquire).toHaveBeenCalledTimes(2);
    expect(api.create).toHaveBeenCalledTimes(2);
    expect(renderer.root.findAllByType('textarea')).toHaveLength(0);
    expect(JSON.stringify(renderer.toJSON())).not.toContain('SECRET');
    expect(release).toHaveBeenCalledWith(expect.objectContaining({ revision: 5 }), false);
  });
  it('待核后显式读取只更新快照，不重发成功或待核行', async () => {
    api.create.mockRejectedValueOnce({ status: 409 });
    await render();
    await prepare();
    await click('仅导入有效行');
    expect(JSON.stringify(renderer.toJSON())).toContain('未执行');
    await click('读取已保存状态');
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.list).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(renderer.toJSON())).toContain('不会重发');
  });
  it('连接切换清空原文与预览，旧材料不在新连接提交', async () => {
    await render();
    await prepare();
    await render({ ...scope, apiBase: 'http://127.0.0.1:10741' });
    expect(renderer.root.findByType('textarea').props.value).toBe('');
    expect(renderer.root.findAllByType('li')).toHaveLength(0);
    expect(api.create).not.toHaveBeenCalled();
  });
  it('关闭在途抽屉先取消请求并保留部分结果', async () => {
    let resolve!: (value: KiloProxyState) => void;
    api.create.mockReturnValue(new Promise<KiloProxyState>((r) => (resolve = r)));
    await render();
    await prepare();
    await click('仅导入有效行');
    const signal = api.create.mock.calls[0][3] as AbortSignal;
    await act(async () => {
      expect(await renderer.root.findByType(Drawer).props.onBeforeClose()).toBe(false);
    });
    expect(signal.aborted).toBe(true);
    await act(async () =>
      resolve({ ...snapshot, revision: 4, proxies: [{ id: 'new' }] as KiloProxyState['proxies'] })
    );
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).toContain('待核');
  });
});

it('实测待核确认后保存，不把HTTP201标成已入池', async () => {
  api.preview.mockResolvedValue({
    status: 'pending',
    reason: 'probe_failed',
    observed_exit_ip: null,
    observed_at: null,
  });
  api.create.mockResolvedValueOnce({
    ...snapshot,
    revision: 4,
    proxies: [
      { id: 'saved-pending', admission_status: 'pending', admission_reason: 'probe_failed' },
    ],
  });
  await render();
  await input('http://USER:SECRET@192.0.2.1:8080');
  await click('逐行预览');
  expect(JSON.stringify(renderer.toJSON())).toContain('确认后仅保存，不入池');
  expect(api.create).not.toHaveBeenCalled();
  await act(async () =>
    renderer.root.findByType('input').props.onChange({ target: { checked: true } })
  );
  await click('仅导入有效行');
  expect(JSON.stringify(renderer.toJSON())).toContain('已保存但未入池');
  expect(JSON.stringify(renderer.toJSON())).not.toContain('SECRET');
});
it('确认期间连接切换废弃已预览材料，不继续创建', async () => {
  await render();
  await prepare();
  await render({ ...scope, managementKey: 'OTHER' });
  expect(button('仅导入有效行').props.disabled).toBe(true);
  await click('仅导入有效行');
  expect(api.create).not.toHaveBeenCalled();
});
it('取消首行预览停止第二行并回读，不开放确认提交', async () => {
  let resolve!: (v: unknown) => void;
  api.preview.mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    })
  );
  await render();
  await input('http://one:80\nhttp://two:80');
  await click('逐行预览');
  await act(async () => {
    renderer.root.findByType(Drawer).props.onBeforeClose();
  });
  await act(async () =>
    resolve({
      status: 'ready',
      reason: '',
      observed_exit_ip: '203.0.113.10',
      observed_at: '2026-11-15T00:00:00Z',
    })
  );
  expect(api.preview).toHaveBeenCalledTimes(1);
  expect(api.list).toHaveBeenCalledTimes(1);
  expect(button('仅导入有效行').props.disabled).toBe(true);
  expect(api.create).not.toHaveBeenCalled();
});
it('预览失败只接纳同连接回读，并保持旧预览不可提交', async () => {
  const fresh = { ...snapshot, revision: 9, runtime_revision: 9 };
  api.preview.mockRejectedValue({ status: 502 });
  api.list.mockResolvedValue(fresh);
  await render();
  await input('http://one:80');
  await click('逐行预览');
  expect(release).toHaveBeenCalledWith(fresh, false);
  expect(JSON.stringify(renderer.toJSON())).toContain('已读取当前状态');
  expect(button('仅导入有效行').props.disabled).toBe(true);
  expect(api.create).not.toHaveBeenCalled();
});
it('预览失败后的异连接回读不替换当前快照', async () => {
  api.preview.mockRejectedValue({ status: 409 });
  api.list.mockResolvedValue({ ...snapshot, revision: 9, connection_ref: 'other' });
  await render();
  await input('http://one:80');
  await click('逐行预览');
  expect(release).toHaveBeenCalledWith(snapshot, true);
  expect(JSON.stringify(renderer.toJSON())).toContain('请刷新后重新预览');
  expect(button('仅导入有效行').props.disabled).toBe(true);
});
