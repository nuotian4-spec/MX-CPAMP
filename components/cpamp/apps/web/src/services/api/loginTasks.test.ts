import { describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), delete: vi.fn() }));
vi.mock('./client', () => ({
  apiClient: client,
  createScopedApiRequestConfig: (scope: { managementKey: string }) => ({
    cpampScopedRequest: true,
    headers: { Authorization: `Bearer ${scope.managementKey}` },
  }),
}));
import {
  loginTasksApi,
  isLoginTaskActive,
  loginTaskLabel,
  loginDiagnosticLabel,
  bpsOutcome,
  bpsOutcomeLabel,
} from './loginTasks';
describe('登录API隔离', () => {
  it('只渲染已知诊断，不渲染原始输入', () => {
    expect(loginDiagnosticLabel({ stage: 'totp', reason: 'security_check' })).toContain('TOTP验证');
    expect(loginDiagnosticLabel({ stage: 'PASSWORD_SECRET', reason: 'COOKIE_SECRET' })).toBe('');
  });
  it('使用Manager根入口而非v0，固定BPS且保留管理员头', () => {
    const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN' };
    loginTasksApi.startBPS(scope);
    expect(client.post).toHaveBeenCalledWith(
      '/oauth/auth-url',
      { provider: 'bps' },
      {
        baseURL: 'https://panel.invalid/api/oauth-login',
        cpampScopedRequest: true,
        headers: { Authorization: 'Bearer ADMIN' },
      }
    );
  });
  it('配置只读查询固定Manager入口并携带当前实例管理员头', () => {
    const scope = { apiBase: 'https://panel.invalid/v0/management', managementKey: 'ADMIN' };
    loginTasksApi.capabilities(scope);
    expect(client.get).toHaveBeenCalledWith('/capabilities', {
      baseURL: 'https://panel.invalid/api/oauth-login',
      cpampScopedRequest: true,
      headers: { Authorization: 'Bearer ADMIN' },
    });
  });
  it('人工验证不是成功或进行中', () => {
    const task = { id: 'id', profile: 'bps' as const, status: 'needs_user_action' as const };
    expect(isLoginTaskActive(task)).toBe(false);
    expect(loginTaskLabel(task)).toContain('查看诊断');
  });
});

describe('安全终态摘要', () => {
  it('保留后端明确保存结果，单独confirmed不代替双布尔事实', () => {
    expect(
      bpsOutcome(
        { status: 'error', error_code: 'credential_commit_failed', save_outcome: 'not_saved' },
        true
      ).save_outcome
    ).toBe('not_saved');
    expect(
      bpsOutcome(
        { status: 'error', error_code: 'oauth_exchange_failed', save_outcome: 'unknown' },
        true
      ).save_outcome
    ).toBe('unknown');
    expect(bpsOutcome({ status: 'ok', save_outcome: 'confirmed' }, true).save_outcome).toBe(
      'unknown'
    );
  });
  it('来源仅展示后端事实，无来源不从HTTP状态猜测过期', () => {
    const raw = {
      status: 'error' as const,
      error_code: 'session_missing_or_expired',
      http_status: 404,
    };
    expect(bpsOutcomeLabel(bpsOutcome(raw, true))).not.toContain('已失效');
    expect(bpsOutcomeLabel(bpsOutcome(raw, true))).not.toContain('来源：');
    expect(bpsOutcomeLabel(bpsOutcome({ ...raw, error_source: 'cpa_session' }, true))).toContain(
      '来源：CPA会话'
    );
    expect(
      bpsOutcomeLabel(bpsOutcome({ ...raw, error_source: 'SECRET_CANARY' }, true))
    ).not.toContain('SECRET_CANARY');
  });
  it('缺字段和恶意值仅显示unknown/null', () => {
    const out = bpsOutcome(
      { status: 'error', error_code: 'SECRET_CANARY', reason: 'SECRET_CANARY', http_status: 600 },
      true
    );
    expect(out.save_outcome).toBe('unknown');
    expect(out.http_status).toBeNull();
    expect(JSON.stringify(out)).not.toContain('SECRET_CANARY');
    expect(bpsOutcomeLabel(out)).not.toContain('官方页面验证');
  });
  it('普通403不假称确定挑战，诊断字段仅白名单显示', () => {
    const label = loginDiagnosticLabel({
      stage: 'web_login',
      reason: 'security_check',
      http_status: 403,
      endpoint_category: 'chatgpt_home',
      security_check_subtype: 'html_forbidden_unclassified',
      challenge_header_present: false,
    });
    expect(label).toContain('具体原因待确认');
    expect(label).toContain('HTTP 403');
    expect(label).toContain('chatgpt_home');
    expect(label).toContain('挑战头：无');
    expect(
      loginDiagnosticLabel({
        http_status: 600,
        endpoint_category: 'SECRET_CANARY',
        security_check_subtype: 'SECRET_CANARY',
      })
    ).not.toContain('SECRET_CANARY');
  });
  it.each([
    ['session_missing_or_expired', true, 'unknown'],
    ['session_missing_or_expired', false, 'not_submitted'],
    ['oauth_exchange_failed', true, 'not_saved'],
    ['oauth_identity_invalid', true, 'not_saved'],
    ['credential_commit_failed', true, 'unknown'],
  ] as const)('区分%s与提交状态', (error_code, submitted, outcome) => {
    const result = bpsOutcome({ status: 'error', error_code }, submitted);
    expect(result.save_outcome).toBe(outcome);
    expect(bpsOutcomeLabel(result)).not.toContain('检查官方页面验证');
  });
});
