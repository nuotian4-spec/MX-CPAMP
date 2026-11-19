// 本地测试窗口：原生 BPS 授权菜单 + 可控的模拟 CPA 管理接口。
// 用法：node joint-tests/bpsNativeMenuWindow.mjs [--port 5299] [--open]
//
// 这不是验收环境。它只把 cpa-bps 0.5.86 的真实原生页面接上一个可切换剧本的本地模拟后端，
// 用来观察提交、失败、停止和结果确认在页面上的真实表现；不会触达任何真实上游，也不会产生 BPS 凭证。
import { execFileSync } from 'node:child_process';
import { createReadStream, existsSync } from 'node:fs';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(import.meta.url);
const zip = process.env.BPS_ZIP || process.argv.find((a) => a.startsWith('--zip='))?.slice(6) || '';
const argPort = Number((process.argv.find((a) => a.startsWith('--port=')) || '').split('=')[1]) || 5299;
const shouldOpen = process.argv.includes('--open');

if (!zip || !existsSync(zip)) {
  console.error(
    `请用 BPS_ZIP 或 --zip= 指定 0.5.86 插件包路径${zip ? `（当前：${zip}）` : ''}。\n` +
      '例如：set BPS_ZIP=D:\\path\\cpa-bps-unified-0.5.86-linux-amd64-binary.zip'
  );
  process.exit(1);
}

// 从 0.5.86 的 cpa-bps.so 里取出原生页 HTML 与样式，和联调测试一致。
const { native, css } = JSON.parse(
  execFileSync(
    process.env.PYTHON || 'python',
    [
      '-B',
      '-c',
      `
import json, sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    lib, = [n for n in z.namelist() if n.endswith('/plugin/dist/cpa-bps.so')]
    b = z.read(lib)
    marker = b.index(b'<textarea id="auto-accounts"')
    a = b.rindex(b'<!doctype html>', 0, marker)
    end = b.index(b'</html>', marker) + len(b'</html>')
    html = b[a:end]
    start_css = b.index(b':root{color-scheme:dark;')
    css = b[start_css:b.index(b'\\x00', start_css)]
    print(json.dumps(dict(native=html.decode(), css=css.decode())))
`,
      zip,
    ],
    { encoding: 'utf8' }
  )
);

// ── 模拟 CPA 管理接口 ───────────────────────────────────────────────────────────
const TOKEN = 'local-window-key';
const RESOURCE_PATH = '/v0/resource/plugins/cpa-bps/index.html';
const CALLBACK =
  'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback';
const SCENARIOS = ['saved', 'unconfirmed', 'rejected', 'missing', 'slow'];
let scenario = 'saved';
let seq = 0;
const sessions = new Map();
const log = [];

const authorizeFor = (state) => {
  const url = new URL('https://auth.openai.com/api/accounts/authorize');
  url.searchParams.set('client_id', 'app_fnr0pYvVwwFDocDumLG3H2Bp');
  url.searchParams.set('redirect_uri', CALLBACK);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('code_challenge_method', 'S256');
  url.searchParams.set('code_challenge', 'x'.repeat(43));
  url.searchParams.set('state', state);
  return url.href;
};

const json = (res, body, status = 200) => {
  const raw = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
    'Content-Length': Buffer.byteLength(raw),
  });
  res.end(raw);
};

const handleManagement = (req, res, url) => {
  const route = url.pathname.slice('/v0/management'.length);
  const auth = req.headers.authorization || '';
  log.push({ route, method: req.method, at: Date.now() });
  if (auth !== `Bearer ${TOKEN}`) return json(res, { error: 'unauthorized' }, 401);

  if (route === '/config') {
    return json(res, {
      'config-version': 8,
      server: { host: '127.0.0.1', port: 0 },
      management: { 'allow-remote': false },
    });
  }
  if (route === '/plugins') {
    return json(res, {
      plugins_enabled: true,
      plugins_dir: 'plugins',
      plugins: [
        {
          id: 'cpa-bps',
          name: 'cpa-bps',
          registered: true,
          enabled: true,
          effective_enabled: true,
          supports_oauth: true,
          oauth_provider: 'cpa-bps',
          menus: [{ path: RESOURCE_PATH, menu: 'BPS 授权', description: '' }],
        },
      ],
    });
  }
  if (route === '/cpa-bps-auth-url') {
    // state 需满足 32–128 位 [A-Za-z0-9_-]；前缀 13 位 + 20 位序号 = 33。
    const state = `window_state_${String(++seq).padStart(20, '0')}`;
    sessions.set(state, { polls: 0, savedChecks: 0, createdAt: Date.now() });
    return json(res, { url: authorizeFor(state), state });
  }
  // 模拟核心未提供自动授权接口：页面应提示并保留手动入口。
  if (scenario === 'missing' && route.startsWith('/cpa-bps/auto-login')) {
    return json(res, { error: 'not found' }, 404);
  }
  if (route === '/cpa-bps/auto-login' && req.method === 'POST') {
    let body = '';
    req.on('data', (chunk) => {
      body += chunk;
      if (body.length > 16384) req.destroy();
    });
    req.on('end', () => {
      let payload = {};
      try {
        payload = JSON.parse(body || '{}');
      } catch {
        return json(res, { status: 'invalid_account_format', message: '账号行格式不正确' });
      }
      const session = sessions.get(payload.state);
      if (!session) return json(res, { status: 'unknown_state', message: '授权会话不存在或已过期' });
      if (payload.cancel === true) {
        session.cancelled = true;
        return json(res, { status: 'cancelled', message: '已停止本批' });
      }
      if (typeof payload.account !== 'string' || !payload.account.trim()) {
        return json(res, { status: 'invalid_account_format', message: '账号行格式不正确' });
      }
      session.account = payload.account;
      session.started = true;
      if (scenario === 'rejected') {
        return json(res, { status: 'credentials_rejected', message: '账号或密码不正确' });
      }
      return json(res, { status: 'pending', message: '已受理，正在登录' });
    });
    return;
  }
  if (route === '/cpa-bps/auto-login' && req.method === 'GET') {
    const state = url.searchParams.get('state') || '';
    const session = sessions.get(state);
    if (!session) return json(res, { status: 'unknown_state', message: '授权会话不存在或已过期' });
    if (session.cancelled) return json(res, { status: 'cancelled', message: '已停止本批' });
    session.polls += 1;
    // slow 剧本一直停在 pending，用来看停止按钮是否只取消、不重提。
    if (scenario === 'slow') return json(res, { status: 'pending', message: '正在登录' });
    if (session.polls < 2) return json(res, { status: 'pending', message: '正在登录' });
    return json(res, { status: 'ready', message: '登录完成，等待 CPA 保存' });
  }
  if (route === '/get-auth-status') {
    const state = url.searchParams.get('state') || '';
    const session = sessions.get(state);
    if (!session) return json(res, { status: 'error', error: 'unknown_state' });
    session.savedChecks += 1;
    if (!session.started) return json(res, { status: 'wait' });
    if (session.cancelled) return json(res, { status: 'error', error: 'cancelled' });
    if (scenario === 'unconfirmed') return json(res, { status: 'ok', saved: true, adopted: false });
    return json(res, { status: 'ok', saved: true, adopted: true });
  }
  return json(res, { error: `mock 未实现 ${route}` }, 404);
};

// ── Vite 开发服务器 ──────────────────────────────────────────────────────────
const { createServer: createViteServer } = await import('vite');
const entry = 'virtual:bps-native-window';
const fixture = `
import React from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import i18n from '@/i18n';
import { useAuthStore } from '@/stores';
import { apiClient } from '@/services/api/client';
import { PluginResourcePage } from '@/features/plugins/PluginResourcePage';

await i18n.changeLanguage('zh-CN');
const scope = { apiBase: location.origin, managementKey: ${JSON.stringify(TOKEN)} };
apiClient.setConfig(scope);
useAuthStore.setState({
  ...scope, isAuthenticated: true, connectionStatus: 'connected', supportsPlugin: true,
  serverVersion: 'local-window (模拟)', rememberPassword: false,
});
const e = React.createElement;
createRoot(document.getElementById('root')).render(
  e(MemoryRouter, { initialEntries: ['/plugin-pages/cpa-bps/0'] },
    e(Routes, null,
      e(Route, { path: '/plugin-pages/:pluginId/:menuIndex', element: e(PluginResourcePage) })))
);
`;

const vite = await createViteServer({
  root,
  configFile: path.join(root, 'vite.config.ts'),
  logLevel: 'error',
  server: { host: '127.0.0.1', port: argPort, strictPort: true, hmr: false },
  plugins: [
    {
      name: 'local-bps-window',
      resolveId: (id) => (id === entry ? '\0' + id : undefined),
      load: (id) => (id === '\0' + entry ? fixture : undefined),
      configureServer(server) {
        // 注册在 configureServer 主体内：先于 Vite 的 SPA fallback 生效。
        server.middlewares.use((req, res, next) => {
          const url = new URL(req.url || '/', 'http://127.0.0.1');
          if (url.pathname === '/' || url.pathname === '/window.html') {
            res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
            return res.end(shell);
          }
          if (url.pathname === '/__scenario') {
            if (req.method === 'POST') {
              let body = '';
              req.on('data', (c) => (body += c));
              req.on('end', () => {
                const next = JSON.parse(body || '{}').scenario;
                if (SCENARIOS.includes(next)) scenario = next;
                json(res, { scenario, scenarios: SCENARIOS });
              });
              return;
            }
            return json(res, { scenario, scenarios: SCENARIOS, requests: log.slice(-40) });
          }
          if (url.pathname === RESOURCE_PATH) {
            res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
            return res.end(native);
          }
          if (url.pathname === '/v0/resource/plugins/cpa-bps/app.css') {
            res.writeHead(200, { 'Content-Type': 'text/css; charset=utf-8' });
            return res.end(css);
          }
          // 插件脚本一律拦掉：本窗口验证的是 Manager 承接，不执行插件页面脚本。
          if (url.pathname.startsWith('/v0/resource/plugins/')) {
            res.writeHead(404, { 'Content-Type': 'text/plain' });
            return res.end('blocked by local window');
          }
          if (url.pathname.startsWith('/v0/management/')) return handleManagement(req, res, url);
          if (url.pathname === '/favicon.ico') {
            res.writeHead(204);
            return res.end();
          }
          return next();
        });
      },
    },
  ],
});

await vite.listen();
const origin = `http://127.0.0.1:${argPort}`;
const shell = await vite.transformIndexHtml(
  '/window.html',
  `<html><head><meta charset="utf-8"><title>BPS 原生授权菜单 · 本地测试窗口</title></head>
   <body style="margin:0">
   <div id="banner" style="font:13px/1.6 system-ui;background:#7a2b12;color:#fff;padding:8px 12px">
     <strong>本地测试窗口，不是验收环境。</strong>
     提交只到本机模拟后端，不触达真实上游，不会产生 BPS 凭证。剧本：

     <span id="scenarios"></span>
     <span id="current" style="opacity:.85"></span>
   </div>
   <div id="root"></div>
   <script>
     const box = document.getElementById('scenarios');
     const current = document.getElementById('current');
     const labels = { saved: '成功保存', unconfirmed: '已保存未注册', rejected: '插件拒绝', missing: '接口缺失', slow: '一直进行中' };
     async function load() {
       const data = await (await fetch('/__scenario')).json();
       box.textContent = '';
       for (const name of data.scenarios) {
         const b = document.createElement('button');
         b.textContent = labels[name] || name;
         b.style.cssText = 'margin:0 4px;padding:2px 8px;font:inherit;cursor:pointer';
         b.disabled = name === data.scenario;
         b.onclick = async () => {
           await fetch('/__scenario', { method: 'POST', body: JSON.stringify({ scenario: name }) });
           await load();
           // 切剧本后重载，确保从干净状态开始本次观察。
           document.querySelector('iframe')?.contentWindow?.location.reload();
         };
         box.appendChild(b);
       }
       current.textContent = ' 当前：' + (labels[data.scenario] || data.scenario);
     }
     load();
   </script>
   <script type="module" src="/@id/__x00__${entry}"></script>
   </body></html>`
);

console.log(`\n本地测试窗口：${origin}/`);
console.log(`模拟后端剧本：${SCENARIOS.join(' / ')}（页头按钮切换，当前 saved）`);
console.log('提醒：这是本地模拟，不是真实授权，不会产生 BPS 凭证。\n');
if (shouldOpen) {
  const { spawn } = await import('node:child_process');
  const opener = process.platform === 'win32' ? 'cmd' : process.platform === 'darwin' ? 'open' : 'xdg-open';
  const args = process.platform === 'win32' ? ['/c', 'start', '', `${origin}/`] : [`${origin}/`];
  spawn(opener, args, { detached: true, stdio: 'ignore' }).unref();
}
