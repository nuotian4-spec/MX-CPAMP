import { describe, expect, it } from 'vitest';
import {
  cpaBpsRemainingPercent,
  cpaBpsResetCount,
  formatCpaBpsGroup,
  formatCpaBpsPercent,
  formatCpaBpsPlan,
  formatCpaBpsTime,
  formatCpaBpsWindow,
  presentCpaBpsMetric,
} from './cpaBpsQuotaPresentation';

describe('插件额度纯展示', () => {
  it.each([
    ['43200m', '30 天窗口'],
    ['10080m', '7 天窗口'],
    ['300m', '5 小时窗口'],
    ['45m', '45 分钟窗口'],
    ['24h', '1 天窗口'],
    ['custom-window', 'custom-window'],
    ['', '额度窗口'],
  ])('窗口%s显示为%s且不推断自然月', (raw, expected) => {
    expect(formatCpaBpsWindow(raw)).toBe(expected);
  });

  it('真实零与未知分开，比例外值不伪装有效额度', () => {
    expect(cpaBpsRemainingPercent(0)).toBe(0);
    expect(cpaBpsRemainingPercent(0.99)).toBe(99);
    expect(cpaBpsRemainingPercent(null)).toBeNull();
    expect(cpaBpsRemainingPercent(-1)).toBeNull();
    expect(cpaBpsRemainingPercent(2)).toBeNull();
    expect(cpaBpsRemainingPercent(Number.NaN)).toBeNull();
    expect(formatCpaBpsPercent(0)).toBe('0%');
    expect(formatCpaBpsPercent(99)).toBe('99%');
    expect(formatCpaBpsPercent(null)).toBe('未知');
  });

  it('重置次数是独立计数，通用指标保留单位且缺值保持未知', () => {
    const metric = {
      key: 'reset_cards_available',
      label: 'Available reset credits',
      value: 0,
      unit: '',
      currency: '',
      format: 'number',
    };
    expect(presentCpaBpsMetric(metric)).toMatchObject({
      label: '可用重置次数',
      value: '0',
      unit: '次',
    });
    expect(presentCpaBpsMetric(metric).hint).toContain('与推理剩余额度不同');
    expect(
      presentCpaBpsMetric({
        ...metric,
        key: 'other',
        label: '其他指标',
        value: null,
        unit: 'units',
      })
    ).toEqual({ label: '其他指标', value: '未知', unit: 'units', hint: '' });
  });

  it('ISO时间转本机月日时分，缺失与非法时间不展示伪值', () => {
    const raw = '2026-12-10T19:32:27Z';
    const date = new Date(raw);
    const pad = (value: number) => String(value).padStart(2, '0');
    expect(formatCpaBpsTime(raw)).toBe(
      `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
    );
    expect(formatCpaBpsTime(undefined)).toBe('未知');
    expect(formatCpaBpsTime('')).toBe('未知');
    expect(formatCpaBpsTime('bad-time')).toBe('未知');
  });

  it('仅翻译已知展示字段，不改未知套餐和分组含义', () => {
    expect(formatCpaBpsGroup('ChatGPT account quota')).toBe('ChatGPT 账号额度');
    expect(formatCpaBpsGroup('Provider Custom')).toBe('Provider Custom');
    expect(
      formatCpaBpsPlan({
        subscription: { plan: 'free', tierName: 'free' },
        groups: [],
        summary: [],
      })
    ).toBe('Free');
    expect(
      formatCpaBpsPlan({
        subscription: { plan: 'custom-plan', tierName: '' },
        groups: [],
        summary: [],
      })
    ).toBe('custom-plan');
  });
});

it.each([undefined, null, -1, 0.5, Number.MAX_SAFE_INTEGER + 1])('非法次数%s不当成0', (value) => {
  const quota = {
    subscription: null,
    groups: [],
    summary:
      value === undefined
        ? []
        : [{ key: 'reset_cards_available', label: '', value, unit: '', format: '', currency: '' }],
  };
  expect(cpaBpsResetCount(quota)).toBeNull();
});
it('显式0、正数与冲突重复次数分开', () => {
  const metric = {
    key: 'reset_cards_available',
    label: '',
    value: 0,
    unit: '',
    format: '',
    currency: '',
  };
  const q = { subscription: null, groups: [], summary: [metric] };
  expect(cpaBpsResetCount(q)).toBe(0);
  expect(cpaBpsResetCount({ ...q, summary: [{ ...metric, value: 3 }] })).toBe(3);
  expect(cpaBpsResetCount({ ...q, summary: [metric, { ...metric, value: 3 }] })).toBeNull();
  expect(
    formatCpaBpsPlan({
      ...q,
      subscription: { plan: 'self_serve_business_prolite', tierName: '完整套餐名称' },
    })
  ).toBe('完整套餐名称');
});
