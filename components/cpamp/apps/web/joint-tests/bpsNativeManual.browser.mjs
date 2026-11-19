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
const { native, css } = JSON.parse(execFileSync(process.env.PYTHON || 'python', ['-B', '-c', `
import json, sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    lib, = [n for n in z.namelist() if n.endswith('/plugin/dist/cpa-bps.so')]
    b = z.read(lib)
    marker = b.index(b'<textarea id="auto-accounts"')
    a = b.rindex(b'<!doctype html>', 0, marker)
    end = b.index(b'</html>', marker) + len(b'</html>')
    html = b[a:end]
    assert b'0.5.86' in html and b'id="callback"' in html
    start_css = b.index(b':root{color-scheme:dark;')
    css = b[start_css:b.index(b'\\x00', start_css)]
    assert b'href="app.css"' in html and b'textarea{' in css and b'.statusbox{' in css
    print(json.dumps(dict(native=html.decode(), css=css.decode())))
`, zip], { encoding: 'utf8' }));
const entry = 'virtual:bps-native-manual-browser-page';
const token = 'local-synthetic-admin';
const autoOnlyEntry = 'virtual:bps-native-auto-only-page';
const fixture = (stateKey) => `
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
    resolveId: id => id === entry || id === autoOnlyEntry ? '\0' + id : undefined,
    load: id => id === '\0' + entry || id === '\0' + autoOnlyEntry ? fixture(id) : undefined,
  }],
});
let browser;
const resourcePath = '/v0/resource/plugins/cpa-bps/index.html';
const callbackBase = 'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback';
const plugins = { plugins_enabled: true, plugins: [{ id: 'cpa-bps', registered: true,
  enabled: true, effective_enabled: true, supports_oauth: true, oauth_provider: 'cpa-bps',
  menus: [{ path: resourcePath, menu: 'BPS 授权' }] }] };
// 每笔授权会话使用独立 state，验证结果查询只认本次。
const autoState = (n) => `auto_synthetic_state_${String(n).padStart(16, '0')}`;
try {
  await vite.listen();
  const origin = `http://127.0.0.1:${vite.httpServer.address().port}`;
  const shell = await vite.transformIndexHtml('/joint-tests/visual.html',
    `<html><head></head><body><div id="root"></div><script type="module" src="/@id/__x00__${entry}"></script></body></html>`);
  const autoShell = await vite.transformIndexHtml('/joint-tests/visual.html',
    `<html><head></head><body><div id="root"></div><script type="module" src="/@id/__x00__${autoOnlyEntry}"></script></body></html>`);
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM_BIN || undefined });
  for (const { layout, adopted } of [
    { layout: 'native', adopted: true },
    { layout: 'native', adopted: false },
    { layout: 'mixed', adopted: true },
    { layout: 'input-only', adopted: false },
  ]) {
    const context = await browser.newContext({ serviceWorkers: 'block' });
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      const resourceHTML = layout === 'native' ? native : await page.evaluate(({ html, layout }) => {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        if (layout === 'mixed') {
          doc.querySelector('#auto-accounts').closest('section').append(doc.querySelector('#start'));
          const extra = doc.createElement('input');
          extra.id = 'extra-setting';
          doc.querySelector('footer').append(extra);
        } else doc.querySelector('#check').remove();
        return '<!doctype html>' + doc.documentElement.outerHTML;
      }, { html: native, layout });
      const state = 'native_manual_synthetic_state_0000000' + Number(adopted);
      const authorize = new URL('https://auth.openai.com/api/accounts/authorize');
      authorize.searchParams.set('redirect_uri', callbackBase);
      authorize.searchParams.set('state', state);
      const callback = `${callbackBase}?code=synthetic-code&state=${state}`;
      const hits = { start: 0, submit: 0, verified: 0, popup: 0 };
      const unexpected = [];
      const errors = [];
      let checkingInput = false, inputRequests = 0;
      context.on('request', () => { if (checkingInput) inputRequests++; });
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
        if (url.pathname === resourcePath) return route.fulfill({ contentType: 'text/html; charset=utf-8', body: resourceHTML });
        if (url.pathname === '/v0/resource/plugins/cpa-bps/app.css') return route.fulfill({ contentType: 'text/css', body: css });
        if (url.pathname.startsWith('/v0/resource/plugins/')) {
          const js = url.pathname.endsWith('.js');
          return route.fulfill({ contentType: js ? 'text/javascript' : 'text/css', body: js ? 'window.vendorScriptRan=true' : '' });
        }
        if (!url.pathname.startsWith('/v0/management/')) return route.continue();
        if (req.headers().authorization !== 'Bearer ' + token) {
          unexpected.push('unauthenticated API'); return route.fulfill({ status: 401 });
        }
        switch (url.pathname.slice('/v0/management/'.length)) {
          case 'plugins': return json(plugins);
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
      assert.equal(await page.locator('iframe').getAttribute('sandbox'), 'allow-same-origin');
      await page.getByText('邮箱密码自动授权：填好后点「开始自动授权」，由插件完成并在本页显示结果。').waitFor();
      if (layout === 'mixed') {
        assert.ok(await frame.locator('section:has(#auto-accounts) #start').isEnabled());
        assert.ok(await frame.locator('#extra-setting').isDisabled());
      }
      const input = frame.locator('textarea#auto-accounts');
      assert.ok(await input.isVisible() && await input.isEditable());
      assert.ok(await input.evaluate((el, html) => {
        const original = new DOMParser().parseFromString(html, 'text/html').querySelector('#auto-accounts');
        return el.placeholder === original.placeholder && el.labels[0].textContent === original.labels[0].textContent &&
          [...el.ownerDocument.styleSheets].some(s => s.href?.endsWith('/app.css') && s.cssRules.length > 0);
      }, native));
      await page.waitForLoadState('networkidle');
      checkingInput = true;
      await input.click();
      assert.ok(await input.evaluate(el => el.ownerDocument.activeElement === el));
      await page.keyboard.type('notebook');
      assert.equal(await input.inputValue(), 'notebook');
      // 模拟粘贴事件和未被取消时的浏览器文本插入，不读写系统剪贴板。
      const pasteText = ' page';
      assert.ok(await input.evaluate((el, text) => {
        const clipboard = new DataTransfer();
        clipboard.setData('text/plain', text);
        return el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
      }, pasteText));
      await page.keyboard.insertText(pasteText);
      assert.equal(await input.inputValue(), 'notebook page');
      // 自动区已接线上报，停止按钮在未开始时保持禁用；无效输入不产生任何管理请求。
      assert.ok(await frame.locator('#auto-start').isEnabled());
      assert.ok(await frame.locator('#auto-stop').isDisabled());
      await frame.locator('#auto-start').evaluate(button => button.click());
      checkingInput = false;
      assert.equal(inputRequests, 0);
      assert.match(await frame.locator('#auto-detail').textContent(), /第 1 行格式不正确/);
      assert.ok(await page.getByRole('link', { name: '面板手动 OAuth 入口' }).isVisible());
      if (layout === 'input-only') {
        assert.ok(await frame.locator('#start').isDisabled());
        assert.ok(await page.getByText('请使用面板入口手动授权。').isVisible());
        await input.evaluate(el => el.ownerDocument.defaultView.dispatchEvent(new Event('pagehide')));
        assert.equal(await input.inputValue(), '');
        assert.deepEqual(unexpected, []); assert.deepEqual(errors, []);
        assert.deepEqual(hits, { start: 0, submit: 0, verified: 0, popup: 0 });
        console.log(JSON.stringify({ native: '0.5.86', layout, inputRequests, pagehideCleared: true, ...hits }));
        continue;
      }
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
      console.log(JSON.stringify({ native: '0.5.86', layout, inputRequests, status: await status.innerText(), adopted, ...hits }));
    } finally { await context.close(); }
  }

  // 自动授权：提交、失败与结果确认。每个场景一笔独立的 state。
  const autoEmail = 'native-auto@example.invalid';
  const autoPassword = 'synthetic-pass';
  const autoLine = `${autoEmail}----${autoPassword}----JBSWY3DPEHPK3PXP`;
  for (const scenario of [
    {
      name: 'auto-saved', submit: { status: 'pending', message: '已受理，正在登录' },
      polls: [{ status: 'pending', message: '正在登录' }, { status: 'ready', message: '登录完成' }],
      saved: { status: 'ok', saved: true, adopted: true },
      expect: /授权成功，CPA 已保存并注册账号/, pollCount: 2, savedCount: 1,
    },
    {
      name: 'auto-unconfirmed', submit: { status: 'pending' },
      polls: [{ status: 'ready', message: '登录完成' }],
      saved: { status: 'ok', saved: true, adopted: false },
      expect: /结果待确认/, reject: /授权成功/, pollCount: 1, savedCount: 1,
    },
    {
      name: 'auto-rejected', submit: { status: 'credentials_rejected', message: '账号或密码不正确' },
      polls: [], saved: null, expect: /账号或密码不正确/, pollCount: 0, savedCount: 0,
    },
    {
      name: 'auto-endpoint-missing', submitStatus: 404,
      polls: [], saved: null, expect: /核心插件未提供自动授权接口/, pollCount: 0, savedCount: 0,
    },
  ]) {
    const context = await browser.newContext({ serviceWorkers: 'block' });
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(20000);
      const state = autoState(scenario.name === 'auto-saved' ? 1 : scenario.name === 'auto-unconfirmed' ? 2 : 3);
      const requests = [];
      const errors = [];
      page.on('pageerror', err => errors.push(err.message));
      await context.route('**/*', async route => {
        const req = route.request(), url = new URL(req.url());
        const json = body => route.fulfill({ json: body });
        if (url.origin !== origin) return route.abort();
        if (url.pathname === '/native-page') return route.fulfill({ contentType: 'text/html', body: autoShell });
        if (url.pathname === resourcePath) return route.fulfill({ contentType: 'text/html; charset=utf-8', body: native });
        if (url.pathname === '/v0/resource/plugins/cpa-bps/app.css') return route.fulfill({ contentType: 'text/css', body: css });
        if (url.pathname.startsWith('/v0/resource/plugins/')) {
          const js = url.pathname.endsWith('.js');
          return route.fulfill({ contentType: js ? 'text/javascript' : 'text/css', body: js ? 'window.vendorScriptRan=true' : '' });
        }
        if (!url.pathname.startsWith('/v0/management/')) return route.continue();
        const routeName = url.pathname.slice('/v0/management/'.length);
        requests.push({ route: routeName, method: req.method(), url: url.href, body: req.postData() });
        switch (routeName) {
          case 'plugins': return json(plugins);
          case 'cpa-bps-auth-url': return json({ url: authorizeFor(state), state });
          case 'cpa-bps/auto-login': {
            if (req.method() === 'POST') {
              const body = req.postDataJSON();
              if (scenario.submitStatus) return route.fulfill({ status: scenario.submitStatus });
              if (body?.state !== state || body?.account !== autoLine) {
                requests.push({ route: 'auto-login-bad-payload', body: JSON.stringify(body) });
                return route.fulfill({ status: 400 });
              }
              return json(scenario.submit);
            }
            if (url.searchParams.get('state') !== state) return route.fulfill({ status: 400 });
            const next = scenario.polls.shift();
            return json(next || { status: 'pending', message: '正在登录' });
          }
          case 'get-auth-status':
            if (url.searchParams.get('state') !== state || !scenario.saved) return route.fulfill({ status: 400 });
            return json(scenario.saved);
          default: return route.fulfill({ status: 404 });
        }
      });
      await page.goto(origin + '/native-page');
      const frame = page.frameLocator('iframe');
      await frame.locator('#auto-start').waitFor({ timeout: 30000 });
      const input = frame.locator('#auto-accounts');
      await input.fill(autoLine);
      await frame.locator('#auto-start').click();
      // 提交后立即清空输入框，密码不留在页面里。
      assert.equal(await input.inputValue(), '');
      await frame.locator('#auto-results li').first().filter({ hasText: scenario.expect }).waitFor();
      const rows = await frame.locator('#auto-results li').allInnerTexts();
      assert.equal(rows.length, 1);
      if (scenario.reject) assert.doesNotMatch(rows[0], scenario.reject);
      assert.ok(rows[0].includes(autoEmail));
      assert.ok(!rows[0].includes(autoPassword));
      const autoRequests = requests.filter(entry => entry.route === 'cpa-bps/auto-login');
      assert.equal(autoRequests.filter(entry => entry.method === 'POST').length, 1);
      assert.equal(autoRequests.filter(entry => entry.method === 'GET').length, scenario.pollCount);
      assert.equal(requests.filter(entry => entry.route === 'get-auth-status').length, scenario.savedCount);
      // 只有那一次提交带账号行；查询、确认与取消都只带 state。
      for (const entry of requests) {
        assert.ok(!entry.url.includes(encodeURIComponent(autoPassword)) && !entry.url.includes(autoPassword));
        const isSubmit = entry.route === 'cpa-bps/auto-login' && entry.method === 'POST' &&
          !JSON.parse(entry.body || '{}').cancel;
        if (!isSubmit) {
          assert.ok(!(entry.body || '').includes(autoPassword), `密码出现在 ${entry.route}`);
        }
      }
      // 本批结束后回到起点：可再次提交，停止按钮禁用。
      assert.ok(await frame.locator('#auto-start').isEnabled());
      assert.ok(await frame.locator('#auto-stop').isDisabled());
      assert.match(await frame.locator('#auto-detail').textContent(), /本批完成，已确认保存并注册 (1|0) 个账号/);
      assert.equal(await frame.locator('body').evaluate(el => !!el.ownerDocument.defaultView.vendorScriptRan), false);
      assert.deepEqual(errors, []);
      console.log(JSON.stringify({ native: '0.5.86', scenario: scenario.name, rows: rows[0],
        posts: autoRequests.filter(entry => entry.method === 'POST').length,
        gets: autoRequests.filter(entry => entry.method === 'GET').length,
        savedChecks: requests.filter(entry => entry.route === 'get-auth-status').length }));
    } finally { await context.close(); }
  }

  // 停止本批：只取消同一笔 state，不再提交。
  {
    const context = await browser.newContext({ serviceWorkers: 'block' });
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(20000);
      const state = autoState(9);
      const requests = [];
      await context.route('**/*', async route => {
        const req = route.request(), url = new URL(req.url());
        const json = body => route.fulfill({ json: body });
        if (url.origin !== origin) return route.abort();
        if (url.pathname === '/native-page') return route.fulfill({ contentType: 'text/html', body: autoShell });
        if (url.pathname === resourcePath) return route.fulfill({ contentType: 'text/html; charset=utf-8', body: native });
        if (url.pathname === '/v0/resource/plugins/cpa-bps/app.css') return route.fulfill({ contentType: 'text/css', body: css });
        if (url.pathname.startsWith('/v0/resource/plugins/')) {
          const js = url.pathname.endsWith('.js');
          return route.fulfill({ contentType: js ? 'text/javascript' : 'text/css', body: js ? 'window.vendorScriptRan=true' : '' });
        }
        if (!url.pathname.startsWith('/v0/management/')) return route.continue();
        const routeName = url.pathname.slice('/v0/management/'.length);
        requests.push({ route: routeName, method: req.method(), body: req.postData() });
        switch (routeName) {
          case 'plugins': return json(plugins);
          case 'cpa-bps-auth-url': return json({ url: authorizeFor(state), state });
          case 'cpa-bps/auto-login': return json({ status: 'pending', message: '正在登录' });
          default: return route.fulfill({ status: 404 });
        }
      });
      await page.goto(origin + '/native-page');
      const frame = page.frameLocator('iframe');
      await frame.locator('#auto-start').waitFor({ timeout: 30000 });
      await frame.locator('#auto-accounts').fill(autoLine);
      await frame.locator('#auto-start').click();
      await frame.locator('#auto-results li').first().filter({ hasText: /正在登录/ }).waitFor();
      await frame.locator('#auto-stop').click();
      await frame.locator('#auto-results li').first().filter({ hasText: /已停止/ }).waitFor({ timeout: 10000 });
      const cancels = requests.filter(entry => entry.route === 'cpa-bps/auto-login' &&
        JSON.parse(entry.body || '{}').cancel === true);
      assert.equal(cancels.length, 1);
      assert.deepEqual(JSON.parse(cancels[0].body), { state, cancel: true });
      assert.equal(requests.filter(entry => entry.route === 'cpa-bps/auto-login' && entry.method === 'POST').length, 2);
      assert.equal(requests.filter(entry => entry.route === 'get-auth-status').length, 0);
      assert.equal(await frame.locator('#auto-accounts').inputValue(), '');
      console.log(JSON.stringify({ native: '0.5.86', scenario: 'auto-stop', cancelled: 1 }));
    } finally { await context.close(); }
  }
} finally { if (browser) await browser.close(); await vite.close(); }

function authorizeFor(state) {
  const authorize = new URL('https://auth.openai.com/api/accounts/authorize');
  authorize.searchParams.set('redirect_uri', callbackBase);
  authorize.searchParams.set('state', state);
  return authorize.href;
}
