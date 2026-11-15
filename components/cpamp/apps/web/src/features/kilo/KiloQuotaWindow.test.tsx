import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { KiloQuotaWindow } from './KiloQuotaWindow';
import { KiloQuotaSummary } from './KiloQuotaSummary';
import type { KiloAccount } from '@/services/api/kilo';

describe('Kilo统一额度窗口', () => {
  it('保留窗口、剩余文字及重置时间，未知不补零', () => {
    const unknown = renderToStaticMarkup(
      <KiloQuotaWindow label="5 小时" percent={null} resetAt={null} />
    );
    expect(unknown).toContain('5 小时');
    expect(unknown).toContain('未知');
    expect(unknown).not.toContain('0%');
    const empty = renderToStaticMarkup(
      <KiloQuotaWindow label="7 天" percent={0} resetAt="2026-11-20T00:00:00Z" />
    );
    expect(empty).toContain('已用尽 / 0%');
    expect(empty).toContain('2026');
    expect(empty).toContain('data-quota-tone="low"');
  });
});

describe('Kilo列表窗口事实', () => {
  const account = {
    id: 'kilo',
    quota_source_summary: {
      kind: 'cpa_codex',
      credential_id: 'source',
      status: 'ready',
      binding_revision: 2,
    },
    quota_snapshot: {
      binding_revision: 2,
      source: { kind: 'cpa_codex', credential_id: 'source', status: 'ready' },
      windows: [],
      query: { status: 'succeeded', observed_at: null },
      stale: false,
    },
  } as unknown as KiloAccount;

  it.each(['missing-source', 'missing-snapshot', 'empty-windows', 'binding-changed'])(
    '%s显示未知窗口，不推断5小时或7天',
    (state) => {
      const value = { ...account };
      if (state === 'missing-source') value.quota_source_summary = undefined;
      if (state === 'missing-snapshot') value.quota_snapshot = undefined;
      if (state === 'binding-changed')
        value.quota_source_summary = { ...account.quota_source_summary!, binding_revision: 3 };
      const html = renderToStaticMarkup(<KiloQuotaSummary account={value} />);
      expect(html).toContain('窗口未知');
      expect(html).toContain('暂无窗口观测');
      expect(html).not.toContain('5 小时');
      expect(html).not.toContain('7 天');
      expect(html).not.toContain('0%');
    }
  );

  it('按观测展示实际窗口时长', () => {
    const value = {
      ...account,
      quota_snapshot: {
        ...account.quota_snapshot!,
        windows: [
          {
            group: 'rate_limit' as const,
            slot: 'primary_window' as const,
            duration_seconds: 7200,
            remaining_percent: 42,
            reset_at: null,
            source: 'api_query' as const,
          },
        ],
      },
    };
    const html = renderToStaticMarkup(<KiloQuotaSummary account={value} />);
    expect(html).toContain('2 小时');
    expect(html).toContain('42%');
    expect(html).not.toContain('窗口未知');
    expect(html).not.toContain('7 天');
  });
});
