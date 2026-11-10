import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';
import { normalizeApiBase } from '@/utils/connection';
import type { LoginProfile, LoginTask } from './loginTasks';

export interface LoginBatchItem extends Omit<LoginTask, 'status'> {
  line_no: number;
  email_hint: string;
  status: LoginTask['status'] | 'pending' | 'skipped';
}
export interface LoginBatch {
  id: string;
  profile: LoginProfile;
  status: 'running' | 'cancelling' | 'completed' | 'cancelled' | 'interrupted';
  error_code?: string;
  items: LoginBatchItem[];
  expires_at?: string;
}
export interface BatchInputItem {
  line_no: number;
  email: string;
  password: string;
  totp_secret: string;
}
export interface BatchPreviewRow {
  line_no: number;
  email_hint: string;
  error?: string;
}
export const clearBatchInput = (items: BatchInputItem[]) => {
  for (const item of items) {
    item.email = '';
    item.password = '';
    item.totp_secret = '';
  }
};
export function parseLoginBatch(raw: string) {
  const items: BatchInputItem[] = [];
  const preview: BatchPreviewRow[] = [];
  const seen = new Set<string>();
  const lines = raw
    .split(/\r?\n/)
    .map((text, index) => ({ text, line_no: index + 1 }))
    .filter(({ text }) => text.trim());
  let error = '';
  if (new TextEncoder().encode(raw).length > 1024 * 1024) error = '每批输入最多 1 MiB';
  else if (lines.length > 100) error = '每批最多 100 行';
  else if (lines.length === 0) error = '请粘贴至少一行账号';
  if (error) return { items, preview, error };
  for (const { text, line_no } of lines) {
    const first = text.indexOf('----');
    const last = text.lastIndexOf('----');
    const email = (first >= 0 ? text.slice(0, first) : '').trim().toLowerCase();
    const password = first >= 0 && last > first ? text.slice(first + 4, last) : '';
    const totp_secret =
      last > first
        ? text
            .slice(last + 4)
            .replace(/\s/g, '')
            .toUpperCase()
        : '';
    const email_hint = /^[^\s@]+@[^\s@]+$/.test(email)
      ? `${[...email][0]}***@${email.split('@')[1]}`
      : '无效邮箱';
    const row: BatchPreviewRow = { line_no, email_hint };
    if (
      new TextEncoder().encode(text).length > 16 * 1024 ||
      first < 1 ||
      last <= first ||
      !/^[^\s@]+@[^\s@]+$/.test(email) ||
      !password ||
      new TextEncoder().encode(password).length > 4096 ||
      !/^[A-Z2-7=]{16,160}$/.test(totp_secret)
    )
      row.error = '格式或材料无效，跳过';
    else if (seen.has(email)) row.error = '重复邮箱，跳过';
    else {
      seen.add(email);
      items.push({ line_no, email, password, totp_secret });
    }
    preview.push(row);
  }
  return { items, preview, error };
}
const config = (scope: ApiClientRequestScope) => ({
  ...createScopedApiRequestConfig(scope),
  baseURL: `${normalizeApiBase(scope.apiBase)}/api/oauth-login`,
});
export const loginBatchesApi = {
  create: (
    input: { batch_id: string; profile: LoginProfile; items: BatchInputItem[] },
    scope: ApiClientRequestScope
  ) => apiClient.post<LoginBatch>('/batches', input, config(scope)),
  get: (id: string, scope: ApiClientRequestScope) =>
    apiClient.get<LoginBatch>(`/batches/${encodeURIComponent(id)}`, config(scope)),
  active: (scope: ApiClientRequestScope) =>
    apiClient.get<LoginBatch | null>('/batches/active', config(scope)),
  latest: (profile: LoginProfile, scope: ApiClientRequestScope) =>
    apiClient.get<LoginBatch | null>('/batches/latest', { ...config(scope), params: { profile } }),
  cancel: (id: string, scope: ApiClientRequestScope) =>
    apiClient.delete<LoginBatch>(`/batches/${encodeURIComponent(id)}`, config(scope)),
};
export const isLoginBatchActive = (batch?: LoginBatch | null) =>
  !!batch && ['running', 'cancelling'].includes(batch.status);
export const batchStatusLabel = (batch: LoginBatch) =>
  ({
    running: '正在逐条登录',
    cancelling: '正在取消，等待当前条目收敛',
    completed: '批次已完成',
    cancelled: '批次已取消',
    interrupted: '批次已中断',
  })[batch.status];
