/**
 * 原生「BPS 授权」菜单里的邮箱密码自动授权区。
 *
 * 页面本身（cpa-bps 0.5.86）由插件提供，这里只做三件事：读取本页输入、按插件接口提交同一笔、
 * 把插件返回的真实进度和 CPA 的保存结果写回本页。提交、失败和结果判定都以插件与 CPA 的
 * 返回为准，不在管理面板里模拟成功。
 */

import type { ApiClientRequestScope } from '@/services/api/client';
import type { OAuthStartResponse, OAuthStatusResponse } from '@/services/api/oauth';
import {
  isBpsAutoLoginPending,
  isBpsAutoLoginSettled,
  type BpsAutoLoginState,
} from '@/services/api/bpsAutoLogin';

/** 与插件页面相同的单批上限。 */
export const BPS_AUTO_LOGIN_MAX_ACCOUNTS = 100;
const POLL_INTERVAL_MS = 1500;
const BATCH_DEADLINE_MS = 270_000;

export interface BpsAccountRow {
  email: string;
  /** 原始行：邮箱----密码----2FA密钥。只在提交时使用，之后立即丢弃。 */
  account: string;
}

export type BpsAccountParse =
  | { ok: true; rows: BpsAccountRow[] }
  | { ok: false; message: string };

const emailPattern = /^[^\s<>@]+@[^\s<>@]+$/;
const secretPattern = /^[A-Z2-7]{16,208}=*$/i;

/**
 * 按插件页面的规则解析账号队列：每行 `邮箱----密码----2FA密钥`。
 * 规则与插件 auto.js 一致，避免管理器放过插件会拒绝的行。
 */
export function parseBpsAccounts(value: string): BpsAccountParse {
  const lines = value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);
  if (lines.length === 0) return { ok: false, message: '请先填写至少一个账号。' };
  if (lines.length > BPS_AUTO_LOGIN_MAX_ACCOUNTS) {
    return { ok: false, message: `每批最多 ${BPS_AUTO_LOGIN_MAX_ACCOUNTS} 个账号，请分批提交。` };
  }
  const emails = new Set<string>();
  const rows: BpsAccountRow[] = [];
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const first = line.indexOf('----');
    const last = line.lastIndexOf('----');
    const email = line.slice(0, first).trim();
    if (
      first < 1 ||
      first === last ||
      last === first + 4 ||
      line.length > 4096 ||
      !emailPattern.test(email)
    ) {
      return { ok: false, message: `第 ${index + 1} 行格式不正确。` };
    }
    const secret = line.slice(last + 4).replace(/ /g, '').trim();
    if (secret && !secretPattern.test(secret)) {
      return { ok: false, message: `第 ${index + 1} 行 2FA 密钥格式不正确。` };
    }
    if (emails.has(email.toLowerCase())) {
      return { ok: false, message: `第 ${index + 1} 行邮箱重复。` };
    }
    emails.add(email.toLowerCase());
    rows.push({ email, account: line });
  }
  return { ok: true, rows };
}

/** 只有 status=ok 且 saved/adopted 都为 true 才算本次授权真的落到凭证里。 */
export function isBpsAuthorizationSaved(value: OAuthStatusResponse) {
  return value.status === 'ok' && value.saved === true && value.adopted === true;
}

export type BpsAutoLoginApi = {
  startAuth: (provider: string, scope: ApiClientRequestScope) => Promise<OAuthStartResponse>;
  getAuthStatus: (state: string, scope: ApiClientRequestScope) => Promise<OAuthStatusResponse>;
  submit: (state: string, account: string, scope: ApiClientRequestScope) => Promise<BpsAutoLoginState>;
  status: (state: string, scope: ApiClientRequestScope) => Promise<BpsAutoLoginState>;
  cancel: (state: string, scope: ApiClientRequestScope) => Promise<BpsAutoLoginState>;
};

export interface BpsAutoLoginContext {
  doc: Document;
  api: BpsAutoLoginApi;
  scope: ApiClientRequestScope;
  /** 校验插件返回的授权链接，与手动授权共用同一份判断。 */
  validStart: (data: OAuthStartResponse) => { state: string; url: string } | null;
  /** 文档仍是本次绑定的那一份。 */
  alive: () => boolean;
  /** 清空并丢弃本页输入。 */
  clearAccounts: () => void;
  signal: AbortSignal;
  wait?: (ms: number) => Promise<void>;
}

export interface BpsAutoLoginBinding {
  ready: boolean;
  disconnect: () => void;
}

const delay = (ms: number) => new Promise<void>((resolve) => window.setTimeout(resolve, ms));

const errorStatus = (error: unknown) => {
  const status = (error as { status?: unknown } | null)?.status;
  return typeof status === 'number' ? status : 0;
};

const errorCode = (error: unknown) => {
  const code = (error as { code?: unknown } | null)?.code;
  return typeof code === 'string' ? code : '';
};

/**
 * 提交后拿不到确定答复时只查询同一笔，不重发。明确的拒绝直接结束该行。
 */
const isAmbiguousSubmit = (error: unknown) => {
  if (errorCode(error) === 'ECONNABORTED' || errorCode(error) === 'ETIMEDOUT') return true;
  const status = errorStatus(error);
  if (status === 0) return true;
  return status === 429 || status >= 500;
};

/** 原生页的自动授权区；控件不完整时保持禁用，不假装可用。 */
export function bindBpsAutoLogin(ctx: BpsAutoLoginContext): BpsAutoLoginBinding {
  const { doc, api, scope, validStart } = ctx;
  const byId = (id: string) => doc.getElementById(id);
  const accounts = byId('auto-accounts') as HTMLTextAreaElement | null;
  const start = byId('auto-start') as HTMLButtonElement | null;
  const stop = byId('auto-stop') as HTMLButtonElement | null;
  const results = byId('auto-results');
  const detail = byId('auto-detail');
  if (
    accounts?.localName !== 'textarea' ||
    start?.localName !== 'button' ||
    stop?.localName !== 'button' ||
    !results ||
    !detail
  ) {
    return { ready: false, disconnect: () => undefined };
  }

  const wait = ctx.wait ?? delay;
  let busy = false;
  let stopped = false;
  let activeState = '';

  const render = () => {
    start.disabled = busy;
    stop.disabled = !busy;
    accounts.disabled = busy;
  };
  const setRow = (node: HTMLElement, email: string, message: string) => {
    // 只写邮箱；密码和 2FA 密钥从不进入页面文本。
    node.textContent = `${email} — ${message}`;
  };
  const say = (message: string) => {
    detail.textContent = message;
  };
  let cancelSent = false;
  const cancelBatch = async () => {
    stopped = true;
    const state = activeState;
    if (!state || cancelSent) return;
    cancelSent = true;
    try {
      await api.cancel(state, scope);
    } catch {
      /* 服务端也有截止时间；不重试、不重放。 */
    }
  };

  const confirmSaved = async (state: string) => {
    const result = await api.getAuthStatus(state, scope);
    if (isBpsAuthorizationSaved(result)) return 'saved' as const;
    if (result.status === 'error') return 'failed' as const;
    return 'unconfirmed' as const;
  };

  const runRow = async (row: BpsAccountRow, node: HTMLElement) => {
    setRow(node, row.email, '正在创建授权会话');
    const session = validStart(await api.startAuth('cpa-bps', scope));
    if (!session) {
      setRow(node, row.email, '授权会话校验失败，已停止本行');
      return false;
    }
    if (stopped || !ctx.alive()) return false;
    activeState = session.state;
    cancelSent = false;

    setRow(node, row.email, '正在启动自动登录');
    let accepted = false;
    try {
      const response = await api.submit(session.state, row.account, scope);
      if (isBpsAutoLoginPending(response.status)) {
        accepted = true;
        setRow(node, row.email, response.message || '已受理，正在登录');
      } else {
        setRow(node, row.email, response.message || '未能启动自动登录');
        return false;
      }
    } catch (error) {
      const status = errorStatus(error);
      // 鉴权失败或接口缺失要中止整批，不能退化成逐行重试。
      if (status === 401 || status === 403 || status === 404 || status === 405) throw error;
      if (!isAmbiguousSubmit(error)) {
        setRow(node, row.email, '插件拒绝了本次自动登录，可改用下方手动授权');
        return false;
      }
      // 结果未确认：只查询同一笔，不重复提交。
      accepted = true;
      setRow(node, row.email, '启动结果未确认，正在检查状态');
    } finally {
      row.account = '';
    }
    if (!accepted) return false;

    let last = '';
    const deadline = Date.now() + BATCH_DEADLINE_MS;
    let settled = false;
    while (!stopped && ctx.alive() && Date.now() < deadline) {
      await wait(POLL_INTERVAL_MS);
      if (stopped || !ctx.alive()) break;
      const progress = await api.status(session.state, scope);
      last = progress.message;
      setRow(node, row.email, progress.message || '正在检查状态');
      if (isBpsAutoLoginSettled(progress.status)) {
        settled = true;
        break;
      }
      if (!isBpsAutoLoginPending(progress.status)) {
        setRow(node, row.email, progress.message || '自动登录未完成');
        return false;
      }
    }
    if (!settled) {
      setRow(
        node,
        row.email,
        stopped ? '已停止；若已完成授权，请到凭证管理核对' : '等待超时，请到凭证管理核对'
      );
      await cancelBatch();
      return false;
    }

    setRow(node, row.email, '正在确认 CPA 保存并注册账号');
    const confirmation = await confirmSaved(session.state);
    if (confirmation === 'saved') {
      setRow(node, row.email, '授权成功，CPA 已保存并注册账号');
      return true;
    }
    setRow(
      node,
      row.email,
      confirmation === 'failed'
        ? `CPA 保存授权失败${last ? `：${last}` : ''}`
        : '结果待确认：CPA 尚未确认保存并注册，请到凭证管理核对'
    );
    return false;
  };

  const runBatch = async () => {
    if (busy) return;
    const parsed = parseBpsAccounts(accounts.value);
    if (!parsed.ok) {
      say(parsed.message);
      return;
    }
    const rows = parsed.rows;
    accounts.value = '';
    busy = true;
    stopped = false;
    results.replaceChildren();
    render();
    const nodes = rows.map((row) => {
      const node = doc.createElement('li');
      setRow(node, row.email, '等待登录');
      results.appendChild(node);
      return node;
    });

    let saved = 0;
    try {
      for (let index = 0; index < rows.length; index += 1) {
        if (stopped || !ctx.alive()) {
          setRow(nodes[index], rows[index].email, '已停止');
          rows[index].account = '';
          continue;
        }
        try {
          if (await runRow(rows[index], nodes[index])) saved += 1;
        } catch (error) {
          const status = errorStatus(error);
          if (status === 404 || status === 405) {
            setRow(nodes[index], rows[index].email, '核心插件未提供自动授权接口');
            say('自动授权接口不可用，请升级插件与核心后重试，或改用下方手动授权。');
            stopped = true;
            break;
          }
          setRow(
            nodes[index],
            rows[index].email,
            status === 401 || status === 403
              ? '管理鉴权未通过，请重新登录面板'
              : '请求未完成，请核对接口状态；未确认保存成功'
          );
          stopped = true;
          break;
        } finally {
          rows[index].account = '';
          activeState = '';
        }
      }
    } finally {
      rows.forEach((row) => {
        row.account = '';
      });
      ctx.clearAccounts();
      busy = false;
      stopped = false;
      activeState = '';
      if (ctx.alive()) {
        render();
        say(
          `本批完成，已确认保存并注册 ${saved} 个账号。未完成的账号可使用下方手动授权。`
        );
      }
    }
  };

  start.addEventListener('click', () => void runBatch(), { signal: ctx.signal });
  stop.addEventListener(
    'click',
    () => {
      if (!busy) return;
      say('正在停止本批；已开始的请求不会中断，可在凭证管理核对。');
      void cancelBatch();
    },
    { signal: ctx.signal }
  );
  // 页面卸载与插件页一致：丢弃输入，并结束本批已开始的那一笔。
  doc.defaultView?.addEventListener(
    'pagehide',
    () => {
      ctx.clearAccounts();
      if (busy) void cancelBatch();
    },
    { signal: ctx.signal }
  );

  accounts.disabled = false;
  accounts.readOnly = false;
  accounts.autocomplete = 'off';
  accounts.spellcheck = false;
  render();

  return {
    ready: true,
    disconnect: () => {
      if (busy) void cancelBatch();
      ctx.clearAccounts();
      accounts.disabled = true;
      start.disabled = true;
      stop.disabled = true;
    },
  };
}
