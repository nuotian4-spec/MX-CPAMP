// node joint-tests/bpsNativeManual.browser.mjs <0.5.86 ZIP>
// 需要项目的 Vite 依赖、Python 3 和带 Chromium 的 playwright-core。
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(import.meta.url);
const { createServer } = await import('vite');
const { chromium } = require(process.env.PLAYWRIGHT_CORE || 'playwright-core');
const zip = process.argv[2];
assert.ok(zip, '请指定 0.5.86 ZIP 路径');
const native = execFileSync(process.env.PYTHON || 'python', ['-B', '-c', `
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    lib, = [n for n in z.namelist() if n.endswith('/plugin/dist/cpa-bps.so')]
    b = z.read(lib)
    marker = b.index(b'<textarea id="auto-accounts"')
    a = b.rindex(b'<!doctype html>', 0, marker)
    end = b.index(b'</html>', marker) + len(b'</html>')
    html = b[a:end]
    assert b'0.5.86' in html and b'id="callback"' in html
    sys.stdout.buffer.write(html)
`, zip], { encoding: 'utf8' });
const entry = 'virtual:bps-native-manual-browser-page';
const token = 'local-synthetic-admin';
const fixture = `
import React from 'react'; import {createRoot} from 'react-dom/client';
import {MemoryRouter, Routes, Route} from 'react-router-dom';
import i18n from '@/i18n'; import {useAuthStore} from '@/stores';
import {apiClient} from '@/services/api/client';
import {PluginResourcePage} from '@/features/plugins/PluginResourcePage';
await i18n.changeLanguage('zh-CN');
const scope = {apiBase: location.origin, managementKey: ${JSON.stringify(token)}};
apiClient.setConfig(scope);
useAuthStore.setState({...scope, isAuthenticated:true, connectionStatus:'connected', supportsPlugin:true});
const e=React.createElement;
createRoot(document.getElementById('root')).render(e(MemoryRouter,{initialEntries:['/plugin-pages/cpa-bps/0']},
  e(Routes,null,e(Route,{path:'/plugin-pages/:pluginId/:menuIndex',element:e(PluginResourcePage)}))));
`;
const vite = await createServer({
  root, configFile: path.join(root, 'vite.config.ts'), logLevel: 'error',
  server: { host: '127.0.0.1', port: 0, hmr: false },
  plugins: [{
    name: 'local-bps-browser-page',
    resolveId: id => id === entry ? '\0' + id : undefined,
    load: id => id === '\0' + entry ? fixture : undefined,
  }],
});
let browser;
const resourcePath = '/v0/resource/plugins/cpa-bps/index.html';
const callbackBase = 'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback';
try {
  await vite.listen();
  const origin = `http://127.0.0.1:${vite.httpServer.address().port}`;
  const shell = await vite.transformIndexHtml('/joint-tests/visual.html',
    `<html><head></head><body><div id="root"></div><script type="module" src="/@id/__x00__${entry}"></script></body></html>`);
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM_BIN || undefined });
  for (const adopted of [true, false]) {
    const context = await browser.newContext({ serviceWorkers: 'block' });
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      const state = 'native_manual_synthetic_state_0000000' + Number(adopted);
      const authorize = new URL('https://auth.openai.com/api/accounts/authorize');
      authorize.searchParams.set('redirect_uri', callbackBase);
      authorize.searchParams.set('state', state);
      const callback = `${callbackBase}?code=synthetic-code&state=${state}`;
      const hits = { start: 0, submit: 0, verified: 0, popup: 0 };
      const unexpected = [];
      const errors = [];
      page.on('pageerror', err => errors.push(err.message));
      await context.route('**/*', async route => {
        const req = route.request(), url = new URL(req.url());
        const json = body => route.fulfill({ json: body });
        if (url.href === authorize.href && req.isNavigationRequest()) {
          hits.popup++;
          return route.fulfill({ contentType: 'text/html', body: '<title>Synthetic authorization</title>' });
        }
        if (url.origin !== origin) { unexpected.push(url.origin); return route.abort(); }
        if (url.pathname === '/native-page') return route.fulfill({ contentType: 'text/html', body: shell });
        if (url.pathname === resourcePath) return route.fulfill({ contentType: 'text/html; charset=utf-8', body: native });
        if (url.pathname.startsWith('/v0/resource/plugins/')) {
          const js = url.pathname.endsWith('.js');
          return route.fulfill({ contentType: js ? 'text/javascript' : 'text/css', body: js ? 'window.vendorScriptRan=true' : '' });
        }
        if (!url.pathname.startsWith('/v0/management/')) return route.continue();
        if (req.headers().authorization !== 'Bearer ' + token) {
          unexpected.push('unauthenticated API'); return route.fulfill({ status: 401 });
        }
        switch (url.pathname.slice('/v0/management/'.length)) {
          case 'plugins': return json({ plugins_enabled: true, plugins: [{ id: 'cpa-bps', registered: true,
            enabled: true, effective_enabled: true, supports_oauth: true, oauth_provider: 'cpa-bps',
            menus: [{ path: resourcePath, menu: 'BPS 授权' }] }] });
          case 'cpa-bps-auth-url': hits.start++; return json({ url: authorize.href, state });
          case 'oauth-callback':
            if (req.method() !== 'POST' || req.postDataJSON()?.provider !== 'cpa-bps' || req.postDataJSON()?.redirect_url !== callback) {
              unexpected.push('callback payload'); return route.fulfill({ status: 400 });
            }
            hits.submit++; return json({ status: 'ok' });
          case 'get-auth-status':
            if (url.searchParams.get('state') !== state) { unexpected.push('wrong state'); return route.fulfill({ status: 400 }); }
            if (!hits.submit) return json({ status: 'wait' });
            hits.verified++; return json({ status: 'ok', saved: true, adopted });
          default: unexpected.push(url.pathname); return route.fulfill({ status: 404 });
        }
      });
      await page.goto(origin + '/native-page');
      const frame = page.frameLocator('iframe');
      try { await frame.locator('#start').waitFor({ timeout: 30000 }); }
      catch (err) { throw new Error(`原生菜单未挂载：${JSON.stringify({ errors, unexpected, page: (await page.locator('body').innerText()).slice(0, 250) })}`, { cause: err }); }
      assert.match(await frame.locator('header').textContent(), /0\.5\.86/);
      assert.ok(!(await page.locator('iframe').getAttribute('sandbox')).split(/\s+/).includes('allow-scripts'));
      assert.ok(await frame.locator('#auto-accounts').isDisabled() || !await frame.locator('#auto-accounts').isVisible());
      await frame.locator('#start').click();
      await frame.locator('#open[href]').waitFor();
      const [popup] = await Promise.all([context.waitForEvent('page', { timeout: 5000 }), frame.locator('#open').click()]);
      await popup.waitForURL(authorize.href);
      await popup.close();
      await frame.locator('#callback').fill(callback);
      await frame.locator('#submit').click();
      const status = frame.locator('#status');
      await status.filter({ hasText: adopted ? /成功|已保存并注册/ : /失败|未完成|未注册|未接管|未确认/ }).waitFor();
      if (!adopted) assert.doesNotMatch(await status.innerText(), /授权成功/);
      assert.equal(await frame.locator('#callback').inputValue(), '');
      assert.equal(await frame.locator('body').evaluate(el => !!el.ownerDocument.defaultView.vendorScriptRan), false);
      assert.equal(hits.start, 1); assert.equal(hits.submit, 1); assert.equal(hits.popup, 1); assert.ok(hits.verified >= 1);
      assert.deepEqual(unexpected, []); assert.deepEqual(errors, []);
      console.log(JSON.stringify({ native: '0.5.86', status: await status.innerText(), adopted, ...hits }));
    } finally { await context.close(); }
  }
} finally { if (browser) await browser.close(); await vite.close(); }
