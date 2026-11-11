import { describe, expect, it } from 'vitest';
import { parseLoginBatch, clearBatchInput } from './loginBatches';
describe('批量输入解析', () => {
  it('按首末分隔符解析并保留密码空格与中间分隔符', () => {
    const parsed = parseLoginBatch(
      ' Person@Example.invalid ---- pass----word ----abcdefghijklmnop'
    );
    expect(parsed.items[0]).toEqual({
      line_no: 1,
      email: 'person@example.invalid',
      password: ' pass----word ',
      totp_secret: 'ABCDEFGHIJKLMNOP',
    });
    expect(JSON.stringify(parsed.preview)).not.toContain('pass');
    expect(parsed.preview[0].email_hint).toBe('p***@example.invalid');
    clearBatchInput(parsed.items);
    expect(parsed.items[0].password).toBe('');
  });
  it('跳过坏行与重复邮箱，保留原行号', () => {
    const parsed = parseLoginBatch(
      'bad\n\na@example.invalid----p----ABCDEFGHIJKLMNOP\nA@example.invalid----p----ABCDEFGHIJKLMNOP'
    );
    expect(parsed.items).toHaveLength(1);
    expect(parsed.items[0].line_no).toBe(3);
    expect(parsed.preview.filter((row) => row.error)).toHaveLength(2);
  });
  it('限制100行、总字节和单行长度', () => {
    expect(parseLoginBatch(Array(101).fill('a').join('\n')).error).toContain('100');
    expect(parseLoginBatch('x'.repeat(1024 * 1024 + 1)).error).toContain('MiB');
    expect(
      parseLoginBatch(`a@b.invalid----${'x'.repeat(16384)}----ABCDEFGHIJKLMNOP`).items
    ).toHaveLength(0);
  });
});
