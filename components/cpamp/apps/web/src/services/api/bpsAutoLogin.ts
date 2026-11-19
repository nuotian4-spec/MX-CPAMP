/**
 * BPS 插件单账号自动授权。
 *
 * cpa-bps 0.5.86 的原生授权页把邮箱密码表单交给 `/cpa-bps/auto-login`：
 * 提交一次账号队列项，按同一 state 查询进度，由插件完成登录并把回调交给 CPA 保存。
 * 这里只复用插件自己的接口，不在管理面板重建登录流程。
 */

import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';

export interface BpsAutoLoginState {
  status: string;
  message: string;
}

const AUTO_LOGIN_PATH = '/cpa-bps/auto-login';
const STATE_PATTERN = /^[A-Za-z0-9_-]{32,128}$/;
// 插件只返回固定状态码；超出范围的响应按未确认处理，不展示原文。
const STATUS_PATTERN = /^[a-z0-9_]{1,80}$/;

const record = (value: unknown): Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};

export function normalizeBpsAutoLogin(value: unknown): BpsAutoLoginState {
  const raw = record(value);
  const status = typeof raw.status === 'string' ? raw.status.trim() : '';
  if (!STATUS_PATTERN.test(status)) {
    throw new Error('自动授权接口未返回可识别的状态，请核对插件与核心版本');
  }
  const message = typeof raw.message === 'string' ? raw.message.trim().slice(0, 500) : '';
  return { status, message };
}

const requireState = (state: string) => {
  if (!STATE_PATTERN.test(state)) throw new Error('授权会话标识不正确，请重新生成');
  return state;
};

export const bpsAutoLoginApi = {
  /** 启动本次单账号自动登录；只有 status=pending 才代表插件已受理。 */
  async submit(
    state: string,
    account: string,
    requestScope: ApiClientRequestScope
  ): Promise<BpsAutoLoginState> {
    const value = account.trim();
    if (!value || value.length > 4096 || /[\r\n\0]/.test(value)) {
      throw new Error('账号行格式不正确，未提交');
    }
    // 密码只出现在这一条提交里，查询与取消只带 state。
    return normalizeBpsAutoLogin(
      await apiClient.post(
        AUTO_LOGIN_PATH,
        { state: requireState(state), account: value },
        createScopedApiRequestConfig(requestScope)
      )
    );
  },

  /** 查询同一笔自动登录的进度；不重新发起。 */
  async status(state: string, requestScope: ApiClientRequestScope): Promise<BpsAutoLoginState> {
    return normalizeBpsAutoLogin(
      await apiClient.get(AUTO_LOGIN_PATH, {
        ...createScopedApiRequestConfig(requestScope),
        params: { state: requireState(state) },
      })
    );
  },

  /** 用户停止本批或页面关闭时结束同一笔；服务端另有截止时间。 */
  async cancel(state: string, requestScope: ApiClientRequestScope): Promise<BpsAutoLoginState> {
    return normalizeBpsAutoLogin(
      await apiClient.post(
        AUTO_LOGIN_PATH,
        { state: requireState(state), cancel: true },
        createScopedApiRequestConfig(requestScope)
      )
    );
  },
};

/** 插件受理后的进度状态：ready/success 表示登录完成，可向 CPA 确认保存结果。 */
export const isBpsAutoLoginSettled = (status: string) => status === 'ready' || status === 'success';

/** 插件已受理但尚未完成。 */
export const isBpsAutoLoginPending = (status: string) => status === 'pending';
