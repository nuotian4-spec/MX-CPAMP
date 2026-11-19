import type { ApiClientRequestScope } from '@/services/api/client';
import { oauthApi, type OAuthStartResponse } from '@/services/api/oauth';

const callbackURL =
  'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback';
const one = (url: URL, key: string) => {
  const values = url.searchParams.getAll(key);
  return values.length === 1 ? values[0] : '';
};

export function validBpsStart(data: OAuthStartResponse) {
  try {
    const url = new URL(data.url);
    if (
      url.origin === 'https://auth.openai.com' && url.pathname === '/api/accounts/authorize' &&
      !url.username && !url.password && !url.hash && data.state &&
      /^[A-Za-z0-9_-]{32,128}$/.test(data.state) && one(url, 'state') === data.state &&
      one(url, 'redirect_uri') === callbackURL
    ) return { state: data.state, url: url.href };
  } catch { /* 非预期响应不生成可用链接。 */ }
  return null;
}

export function validBpsCallback(value: string, state: string) {
  try {
    if (!state || value.length > 16384) return null;
    const url = new URL(value.trim());
    if (
      `${url.origin}${url.pathname}` === callbackURL && !url.username && !url.password &&
      !url.hash && !url.searchParams.has('error') && one(url, 'state') === state &&
      one(url, 'code').trim()
    ) return url.href;
  } catch { /* 不回显含授权码的输入。 */ }
  return null;
}

type ManualApi = Pick<typeof oauthApi, 'startAuth' | 'submitCallback' | 'getAuthStatus'>;

/** 保留插件原生 DOM；事件处理和管理请求只在 Manager 窗口中执行。 */
export function bindBpsManualMenu(
  frame: HTMLIFrameElement,
  expectedURL: string,
  scope: ApiClientRequestScope,
  api: ManualApi = oauthApi,
): (() => void) | null {
  frame.inert = true;
  const host = window;
  const url = new URL(expectedURL, host.location.href);
  const doc = frame.contentDocument;
  if (
    !doc || !scope.managementKey || doc.URL !== url.href || url.origin !== host.location.origin ||
    frame.getAttribute('sandbox') !== 'allow-same-origin'
  ) return null;

  doc.querySelectorAll('input,textarea,button,select').forEach((el) => el.setAttribute('disabled', ''));
  doc.querySelectorAll('a[href]').forEach((el) => el.removeAttribute('href'));
  const find = <K extends keyof HTMLElementTagNameMap>(id: string, tag: K) => {
    const el = doc.getElementById(id);
    return el?.localName === tag ? el as HTMLElementTagNameMap[K] : null;
  };
  const start = find('start', 'button'), reset = find('reset', 'button'), open = find('open', 'a');
  const link = find('link', 'textarea'), callback = find('callback', 'textarea');
  const copy = find('copy', 'button'), submit = find('submit', 'button'), check = find('check', 'button');
  const status = doc.getElementById('status'), detail = doc.getElementById('detail');
  if (!start || !reset || !open || !link || !callback || !copy || !submit || !check || !status || !detail) return null;

  let session: ReturnType<typeof validBpsStart> = null;
  let busy = false, submitted = false;
  const lifetime = new AbortController();
  const current = () => !lifetime.signal.aborted && frame.contentDocument === doc;
  const say = (title: string, text = '') => { status.textContent = title; detail.textContent = text; };
  const render = () => {
    start.disabled = busy || !!session;
    reset.disabled = busy;
    link.disabled = !session; link.readOnly = true; link.value = session?.url ?? '';
    copy.disabled = !session;
    open.classList.toggle('disabled', !session);
    open.setAttribute('aria-disabled', String(!session));
    if (session) open.setAttribute('href', session.url); else open.removeAttribute('href');
    callback.disabled = busy || !session || submitted;
    submit.disabled = callback.disabled;
    check.disabled = busy || !session;
  };
  const clear = () => { session = null; submitted = false; callback.value = ''; };
  const action = (el: HTMLElement, fn: () => void) =>
    el.addEventListener('click', (event) => {
      event.preventDefault();
      if (current()) fn();
    }, { signal: lifetime.signal });
  const run = async (fn: () => Promise<void>) => {
    if (busy) return;
    busy = true; render();
    try { await fn(); }
    catch {
      if (current()) say('请求未确认', session ? '可点击“检查授权状态”；请勿重复提交回调。' : '请重新生成链接。');
    } finally {
      if (current()) { busy = false; render(); }
    }
  };
  const probe = async (state: string) => {
    const result = await api.getAuthStatus(state, scope);
    if (!current()) return;
    if (result.status === 'ok' && result.saved === true && result.adopted === true) {
      clear(); say('授权成功，CPA 已保存并注册账号', '可返回凭证管理查看。');
    } else if (result.status === 'error') {
      clear(); say('授权未完成', '请重新生成链接。');
    } else {
      say('尚未确认保存并注册', '可稍后再次检查授权状态。');
    }
  };
  action(start, () => void run(async () => {
    if (session) return;
    const response = await api.startAuth('cpa-bps', scope);
    if (!current()) return;
    session = validBpsStart(response);
    say(session ? '授权链接已生成' : '授权链接校验失败', session ? '打开链接登录，将本次完整回调地址粘贴到下方。' : '请使用面板手动 OAuth 入口。');
  }));
  action(open, () => { if (session) host.open(session.url, '_blank', 'noopener,noreferrer'); });
  action(copy, () => {
    if (!session) return;
    link.focus(); link.select();
    void host.navigator.clipboard?.writeText(session.url).catch(() => {
      if (current()) say('链接已选中，可手动复制');
    });
  });
  action(reset, () => { if (!busy) { clear(); render(); say('尚未开始授权'); } });
  action(submit, () => void run(async () => {
    if (!session || submitted) return;
    const { state } = session;
    const value = validBpsCallback(callback.value, state);
    callback.value = '';
    if (!value) { say('回调与本次授权不匹配', '请粘贴本次登录的完整回调地址。'); return; }
    submitted = true;
    await api.submitCallback('cpa-bps', value, scope);
    if (current()) await probe(state);
  }));
  action(check, () => void run(async () => { if (session) await probe(session.state); }));
  clear(); render(); frame.inert = false;
  return () => {
    lifetime.abort();
    clear();
    link.value = ''; open.removeAttribute('href'); frame.inert = true;
  };
}
