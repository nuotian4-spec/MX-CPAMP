import { describe, expect, it } from 'vitest';
import {
  BPS_AUTO_LOGIN_MAX_ACCOUNTS,
  isBpsAuthorizationSaved,
  parseBpsAccounts,
} from './bpsAutoLoginMenu';

const secret = 'JBSWY3DPEHPK3PXP';

describe('原生页邮箱密码授权队列', () => {
  it('接受插件格式，保留原始行用于一次性提交', () => {
    const parsed = parseBpsAccounts(
      [`user@example.com----pass word----${secret}`, 'second@example.com----pw----', ''].join('\n')
    );
    expect(parsed).toEqual({
      ok: true,
      rows: [
        { email: 'user@example.com', account: `user@example.com----pass word----${secret}` },
        { email: 'second@example.com', account: 'second@example.com----pw----' },
      ],
    });
  });

  it('拒绝空队列、超额、缺密码、重复邮箱和非法 2FA 密钥', () => {
    expect(parseBpsAccounts('   ')).toEqual({ ok: false, message: '请先填写至少一个账号。' });
    const many = Array.from({ length: BPS_AUTO_LOGIN_MAX_ACCOUNTS + 1 }, (_, i) => `u${i}@example.com----p----`);
    expect(parseBpsAccounts(many.join('\n'))).toEqual({
      ok: false,
      message: `每批最多 ${BPS_AUTO_LOGIN_MAX_ACCOUNTS} 个账号，请分批提交。`,
    });
    for (const [line, message] of [
      ['user@example.com--------', '第 1 行格式不正确。'],
      ['user@example.com', '第 1 行格式不正确。'],
      ['example.com----pw----', '第 1 行格式不正确。'],
      [`user@example.com----pw----${secret}1`, '第 1 行 2FA 密钥格式不正确。'],
    ] as const) {
      expect(parseBpsAccounts(line)).toEqual({ ok: false, message });
    }
    expect(parseBpsAccounts(['a@example.com----p----', 'A@example.com----p----'].join('\r\n'))).toEqual({
      ok: false,
      message: '第 2 行邮箱重复。',
    });
  });

  it('只有已保存且已注册才算授权成功', () => {
    expect(isBpsAuthorizationSaved({ status: 'ok', saved: true, adopted: true })).toBe(true);
    for (const value of [
      { status: 'ok' as const, saved: true, adopted: false },
      { status: 'ok' as const, saved: false, adopted: true },
      { status: 'ok' as const },
      { status: 'wait' as const, saved: true, adopted: true },
      { status: 'error' as const, saved: true, adopted: true },
    ]) expect(isBpsAuthorizationSaved(value)).toBe(false);
  });
});
