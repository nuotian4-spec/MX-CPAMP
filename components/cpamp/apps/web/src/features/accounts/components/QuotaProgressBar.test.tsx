import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { QuotaProgressBar } from './QuotaProgressBar';

describe('凭证剩余额度色阶', () => {
  it.each([
    [100, 'high'],
    [51, 'high'],
    [50, 'medium'],
    [20, 'medium'],
    [19, 'low'],
    [0, 'low'],
    [null, 'unknown'],
    [NaN, 'unknown'],
    [Infinity, 'unknown'],
  ] as const)('剩余 %s 显示 %s', (percent, tone) => {
    const html = renderToStaticMarkup(
      <QuotaProgressBar percent={percent} highThreshold={50} mediumThreshold={20} />
    );
    expect(html).toContain(`data-quota-tone="${tone}"`);
    if (percent === 0) expect(html).toContain('已用尽 / 0%');
    if (tone === 'unknown') {
      expect(html).toContain('额度未知');
      expect(html).not.toContain('width:0%');
    }
  });
});
