/// <reference types="node" />
import { createServer, type Server, type ServerResponse } from 'node:http';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { BpsLicensePanel } from '../src/features/plugins/BpsLicensePanel';
import { Button } from '@/components/ui/Button';
import { apiClient } from '@/services/api/client';
import {
  createBpsLicenseApi,
  normalizeBpsLicense,
  classifyBpsLicenseError,
} from '@/services/api/bpsLicense';

vi.mock('@/features/demo/demoMode', async (original) => ({
  ...(await original<typeof import('@/features/demo/demoMode')>()),
  isDemoMode: () => false,
}));
vi.mock('@/components/ui/Drawer', () => ({
  Drawer: ({
    open,
    children,
    onBeforeClose,
  }: {
    open: boolean;
    children: React.ReactNode;
    onBeforeClose: () => void;
  }) =>
    open ? (
      <section data-drawer>
        <button onClick={onBeforeClose}>关闭许可详情</button>
        {children}
      </section>
    ) : null,
}));

let server: Server;
let base: string;
let renderer: ReactTestRenderer | undefined;
let requests: { method: string; path: string; authorization: string; body: unknown }[];
let status: Record<string, unknown>;
let statusCode: number;
let pending: ServerResponse[];
let holdGet: boolean;
let holdWrite: boolean;
let failWrite: boolean;
const secret = 'BPS-11223344-55667788-99AABBCC-DDEEFF00';
const proxySecret = 'http://synthetic-user:synthetic-password@proxy.invalid:80';
const initialStatus = () => ({
  required: true,
  active: false,
  code: 'license_required',
  key_hint: 'BPS-****-EEFF00',
  bound_ip: '127.0.0.1',
  license_expires_at: '2026-12-01T00:00:00Z',
  expires_at: '2026-12-01T00:00:00Z',
});
const respond = (response: ServerResponse, code: number, payload: unknown) => {
  response.writeHead(code, { 'Content-Type': 'application/json' });
  response.end(JSON.stringify(payload));
};
const scope = (key = 'synthetic-manager-alpha') => ({ apiBase: base, managementKey: key });
const text = () => JSON.stringify(renderer?.toJSON());
const button = (label: string) =>
  renderer!.root.findAllByType(Button).find((item) => item.props.children === label) ??
  renderer!.root.findAllByType('button').find((item) => item.children.includes(label))!;
const waitFor = async (check: () => boolean) => {
  for (let i = 0; i < 100 && !check(); i += 1)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5));
    });
  expect(check()).toBe(true);
};
const mount = async (registered = true, connected = true) => {
  await act(async () => {
    renderer = create(
      <BpsLicensePanel scope={scope()} registered={registered} connected={connected} />
    );
  });
};
const open = async () => {
  await act(async () => {
    button('管理许可').props.onClick();
  });
  await waitFor(() => Boolean(renderer!.root.findAllByType('input')[0]?.props.disabled === false));
};

beforeAll(async () => {
  server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk) => {
      body += chunk;
    });
    request.on('end', () => {
      requests.push({
        method: request.method || '',
        path: request.url || '',
        authorization: request.headers.authorization || '',
        body: body ? JSON.parse(body) : undefined,
      });
      if (request.method === 'GET') {
        if (holdGet && request.headers.authorization === 'Bearer synthetic-manager-alpha')
          pending.push(response);
        else respond(response, statusCode, status);
      } else if (holdWrite) pending.push(response);
      else if (failWrite) response.destroy();
      else respond(response, 200, { accepted: true });
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('缺少本地监听地址');
  base = `http://127.0.0.1:${address.port}`;
});
beforeEach(() => {
  requests = [];
  pending = [];
  status = initialStatus();
  statusCode = 200;
  holdGet = false;
  holdWrite = false;
  failWrite = false;
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', { dispatchEvent: vi.fn() });
  apiClient.setConfig(scope());
});
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  for (const response of pending) response.destroy();
  vi.unstubAllGlobals();
});
afterAll(async () => {
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve()))
  );
});

describe('BPS 许可组件与正式 Axios 管理客户端', () => {
  it('未注册仍展示摘要，不请求许可，也不因未激活隐藏入口', async () => {
    await mount(false);
    expect(text()).toContain('插件未注册，许可未核实');
    expect(button('管理许可')).toBeDefined();
    expect(requests).toHaveLength(0);
  });
  it('状态读取与打开详情只 GET，明确未激活允许当次激活', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    expect(
      requests.every(
        (item) => item.method === 'GET' && item.path === '/v0/management/cpa-bps/license'
      )
    ).toBe(true);
    expect(renderer!.root.findByType('input').props.type).toBe('password');
    expect(button('激活许可').props.disabled).toBe(true);
    await act(async () => {
      renderer!.root.findByType('input').props.onChange({ target: { value: secret } });
    });
    expect(button('激活许可').props.disabled).toBe(false);
  });
  it('双击仅一次激活 POST，正文固定，提交立即清空且响应错误不回显秘密', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    holdWrite = true;
    await act(async () => {
      renderer!.root.findByType('input').props.onChange({ target: { value: secret } });
    });
    const submit = button('激活许可').props.onClick;
    await act(async () => {
      submit();
      submit();
    });
    await waitFor(() => pending.length === 1);
    const writes = requests.filter((item) => item.method === 'POST');
    expect(writes).toEqual([
      {
        method: 'POST',
        path: '/v0/management/cpa-bps/license/activate',
        authorization: 'Bearer synthetic-manager-alpha',
        body: { license_key: secret },
      },
    ]);
    expect(renderer!.root.findByType('input').props.value).toBe('');
    await act(async () => {
      respond(pending.shift()!, 500, { error: `${secret} ${proxySecret}` });
    });
    await waitFor(() => text().includes('操作结果未确认'));
    await waitFor(() => button('读取状态').props.disabled === false);
    expect(text()).not.toContain(secret);
    expect(text()).not.toContain('synthetic-password');
    expect(requests.filter((item) => item.method === 'POST')).toHaveLength(1);
    expect(requests[requests.length - 1]?.method).toBe('GET');
  });
  it('刷新明确点击仅发一次空对象 POST，断网结果不明只 GET 核实', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    failWrite = true;
    await act(async () => {
      button('刷新许可').props.onClick();
    });
    await waitFor(() => text().includes('操作结果未确认'));
    await waitFor(() => button('读取状态').props.disabled === false);
    expect(requests.filter((item) => item.method === 'POST')).toEqual([
      {
        method: 'POST',
        path: '/v0/management/cpa-bps/license/refresh',
        authorization: 'Bearer synthetic-manager-alpha',
        body: {},
      },
    ]);
    expect(requests[requests.length - 1]?.method).toBe('GET');
  });
  it('连接切换清理输入，旧读取迟到不污染新连接', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    await act(async () => {
      renderer!.root.findByType('input').props.onChange({ target: { value: secret } });
    });
    holdGet = true;
    await act(async () => {
      button('读取状态').props.onClick();
    });
    await waitFor(() => pending.length === 1);
    status = { ...initialStatus(), active: true, code: 'active' };
    apiClient.setConfig(scope('synthetic-manager-beta'));
    await act(async () => {
      renderer!.update(
        <BpsLicensePanel scope={scope('synthetic-manager-beta')} registered connected />
      );
    });
    await waitFor(() => text().includes('商业许可有效'));
    await act(async () => {
      respond(pending.shift()!, 200, initialStatus());
    });
    expect(text()).toContain('商业许可有效');
    expect(text()).not.toContain(secret);
    expect(renderer!.root.findAllByType('input')).toHaveLength(0);
    expect(requests[requests.length - 1]?.authorization).toBe('Bearer synthetic-manager-beta');
  });
  it('写请求超时仅 GET 核实，不重放 POST', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    apiClient.setConfig({ ...scope(), timeout: 30 });
    holdWrite = true;
    await act(async () => {
      button('刷新许可').props.onClick();
    });
    await waitFor(() => text().includes('操作结果未确认'));
    await waitFor(() => button('读取状态').props.disabled === false);
    expect(requests.filter((item) => item.method === 'POST')).toHaveLength(1);
    expect(requests[requests.length - 1]?.method).toBe('GET');
  });
  it('关闭详情与退出清理输入并忽略在途写结果，不追加重放或核实', async () => {
    await mount();
    await waitFor(() => text().includes('商业许可待激活'));
    await open();
    holdWrite = true;
    await act(async () => {
      renderer!.root.findByType('input').props.onChange({ target: { value: secret } });
    });
    await act(async () => {
      button('激活许可').props.onClick();
    });
    await waitFor(() => pending.length === 1);
    await act(async () => {
      button('关闭许可详情').props.onClick();
    });
    const count = requests.length;
    await act(async () => {
      respond(pending.shift()!, 200, { accepted: true });
    });
    expect(requests.length).toBe(count);
    expect(text()).not.toContain(secret);
    await act(async () => {
      renderer!.update(<BpsLicensePanel scope={scope()} registered connected={false} />);
    });
    expect(text()).toContain('管理连接未就绪');
  });
  it.each([
    [404, '许可接口未提供'],
    [200, '响应缺失或格式未知'],
  ] as const)('区分接口 %s 失败，不变成未激活', async (code, label) => {
    statusCode = code;
    status = {};
    await mount();
    await waitFor(() => text().includes(label));
    expect(text()).not.toContain('商业许可待激活');
  });
  it('未知代码与供应商错误不直接显示，完整授权码不冒充 key_hint', () => {
    const result = normalizeBpsLicense({
      ...initialStatus(),
      code: secret,
      key_hint: secret,
      last_error: proxySecret,
      bound_ip: proxySecret,
    });
    expect(JSON.stringify(result)).not.toContain(secret);
    expect(JSON.stringify(result)).not.toContain(proxySecret);
    expect(result).toMatchObject({ code: 'unknown', keyHint: '', boundIp: '', hasLastError: true });
    expect(classifyBpsLicenseError({ code: 'ECONNABORTED', message: secret })).toBe('timeout');
  });
  it('创建时捕获管理作用域，之后全局切换不改变已建客户端目标', async () => {
    const captured = scope();
    const api = createBpsLicenseApi(captured);
    captured.managementKey = 'mutated';
    apiClient.setConfig(scope('synthetic-manager-beta'));
    await api.refresh();
    expect(requests[0]).toMatchObject({
      authorization: 'Bearer synthetic-manager-alpha',
      method: 'POST',
      body: {},
    });
  });
});
