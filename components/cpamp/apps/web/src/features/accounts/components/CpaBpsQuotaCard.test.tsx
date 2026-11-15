import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CpaBpsQuotaCard } from './CpaBpsQuotaCard';
import { QuotaProgressBar } from './QuotaProgressBar';
import { formatCpaBpsTime } from '../model/cpaBpsQuotaPresentation';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
vi.mock('@/services/api/cpaBpsQuota', () => ({ cpaBpsQuotaApi: { query: vi.fn() } }));
const scope = { apiBase: 'https://a.invalid', managementKey: 'key' };
const file = { name: 'bps.json', id: 'auth-a', provider: 'cpa-bps', auth_index: 'a' };
let renderer: ReactTestRenderer;
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  useQuotaStore.getState().clearQuotaCache();
  vi.clearAllMocks();
});
const mount = async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  await act(async () => {
    renderer = create(<CpaBpsQuotaCard file={file} scope={scope} />);
  });
};
const result: Awaited<ReturnType<typeof cpaBpsQuotaApi.query>> = {
  provider: 'cpa-bps' as const,
  source: 'cpa_bps_plugin_quota' as const,
  auth_index: 'a',
  auth_id: 'auth-a',
  queried_at: '2026-11-12T00:00:00Z',
  quota: { subscription: null, summary: [], groups: [] },
};
const textContent = (node: ReactTestInstance): string =>
  node.children.map((child) => (typeof child === 'string' ? child : textContent(child))).join('');

const query = async () => {
  await act(async () => {
    await renderer.root.findAllByType('button')[0].props.onClick();
  });
};

describe('插件额度卡', () => {
  it('窗口卡显示99%和30天，重置次数0独立展示且时间使用本机格式', async () => {
    const resetTime = '2026-12-10T19:32:27Z';
    vi.mocked(cpaBpsQuotaApi.query).mockResolvedValue({
      ...result,
      quota: {
        subscription: { plan: 'free', tierName: 'free' },
        summary: [
          {
            key: 'reset_cards_available',
            label: 'Available reset credits',
            value: 0,
            unit: '',
            format: 'number',
            currency: '',
          },
        ],
        groups: [
          {
            displayName: 'ChatGPT account quota',
            buckets: [{ window: '43200m', remainingFraction: 0.99, resetTime, description: '' }],
          },
        ],
      },
    });
    await mount();
    await query();
    const text = textContent(renderer.root);
    expect(text).toContain('Free');
    expect(text).toContain('ChatGPT 账号额度');
    expect(text).toContain('30 天窗口');
    expect(text).toContain('99%');
    expect(text).not.toContain('43200m');
    expect(text).not.toContain('Available reset credits');
    expect(text).not.toContain('月');
    expect(text).toContain(formatCpaBpsTime(resetTime));
    expect(text).not.toContain(resetTime);
    expect(
      renderer.root
        .findByProps({ 'data-plugin-quota-metric': 'reset_cards_available' })
        .findByType('strong').children
    ).toEqual(['0']);
    expect(text).toContain('可用重置次数');
    expect(text).toContain('与推理剩余额度不同');
    expect(renderer.root.findByProps({ role: 'progressbar' }).props['aria-valuenow']).toBe(99);
    expect(renderer.root.findByType(QuotaProgressBar).props.percent).toBe(99);
    expect(renderer.root.findAllByType('button')).toHaveLength(2);
  });

  it('多窗口保留真实0，未知不画零进度，缺失时间保持未知', async () => {
    vi.mocked(cpaBpsQuotaApi.query).mockResolvedValue({
      ...result,
      quota: {
        subscription: null,
        summary: [],
        groups: [
          {
            displayName: 'ChatGPT account quota',
            buckets: [
              { window: '300m', remainingFraction: 0, resetTime: '', description: '' },
              { window: '10080m', remainingFraction: null, resetTime: 'bad-time', description: '' },
            ],
          },
          {
            displayName: '模型专用窗口',
            buckets: [
              {
                window: 'custom-window',
                remainingFraction: 0.25,
                resetTime: '',
                description: '模型范围说明',
              },
            ],
          },
        ],
      },
    });
    await mount();
    await query();
    const zero = renderer.root.findByProps({ 'data-plugin-quota-window': '300m' });
    expect(textContent(zero)).toContain('0%');
    expect(zero.findByProps({ role: 'progressbar' }).props['aria-valuenow']).toBe(0);
    const unknown = renderer.root.findByProps({ 'data-plugin-quota-window': '10080m' });
    expect(textContent(unknown)).toContain('未知');
    expect(unknown.findAllByProps({ role: 'progressbar' })).toHaveLength(0);
    expect(unknown.findAllByType(QuotaProgressBar)).toHaveLength(0);
    expect(renderer.root.findAllByProps({ role: 'progressbar' })).toHaveLength(2);
    expect(textContent(renderer.root)).toContain('模型专用窗口');
    expect(textContent(renderer.root)).toContain('模型范围说明');
    expect(textContent(renderer.root)).not.toContain('bad-time');
  });

  it('查询中禁用按钮，错误保留旧快照并标明上次结果', async () => {
    vi.mocked(cpaBpsQuotaApi.query).mockResolvedValueOnce({
      ...result,
      quota: {
        subscription: null,
        summary: [],
        groups: [
          {
            displayName: '窗口',
            buckets: [{ window: '1d', remainingFraction: 0.99, resetTime: '', description: '' }],
          },
        ],
      },
    });
    await mount();
    await query();
    let reject!: (reason: Error) => void;
    vi.mocked(cpaBpsQuotaApi.query).mockReturnValueOnce(
      new Promise((_, fail) => {
        reject = fail;
      })
    );
    let pending!: Promise<unknown>;
    act(() => {
      pending = renderer.root.findAllByType('button')[0].props.onClick();
    });
    expect(renderer.root.findAllByType('button')[0].props.disabled).toBe(true);
    expect(textContent(renderer.root)).toContain('查询中');
    await act(async () => {
      reject(new Error('查询超时'));
      await pending;
    });
    expect(textContent(renderer.root.findByProps({ role: 'alert' }))).toContain(
      '额度未知：查询超时'
    );
    expect(textContent(renderer.root)).toContain(formatCpaBpsTime(result.queried_at));
    expect(textContent(renderer.root)).toContain('99%');
    expect(textContent(renderer.root)).toContain('上次额度快照');
    expect(renderer.root.findAllByProps({ role: 'progressbar' })).toHaveLength(1);
    expect(renderer.root.findAllByType('button')[0].props.disabled).toBe(false);
  });

  it('不自动查询；空额度保持未知且没有重置入口', async () => {
    await mount();
    expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
    vi.mocked(cpaBpsQuotaApi.query).mockResolvedValue(result);
    await act(async () => {
      await renderer.root.findAllByType('button')[0].props.onClick();
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('当前额度未知');
    expect(text).not.toContain('100%');
    expect(renderer.root.findAllByType('button')).toHaveLength(2);
  });
  it('连接切换后迟到额度不污染当前账号', async () => {
    let resolve!: (value: typeof result) => void;
    vi.mocked(cpaBpsQuotaApi.query).mockReturnValue(new Promise((done) => (resolve = done)));
    await mount();
    let pending!: Promise<void>;
    act(() => {
      pending = renderer.root.findAllByType('button')[0].props.onClick();
    });
    await act(async () => {
      renderer.update(
        <CpaBpsQuotaCard file={file} scope={{ ...scope, apiBase: 'https://b.invalid' }} />
      );
      resolve({
        ...result,
        quota: { ...result.quota, subscription: { plan: 'OLD_PLAN', tierName: '' } },
      });
      await pending;
    });
    expect(JSON.stringify(renderer.toJSON())).not.toContain('OLD_PLAN');
  });
});

it('B新增:次数入口复用综合Fetch，并发点击只调用一次', async () => {
  let finish!: (value: typeof result) => void;
  vi.mocked(cpaBpsQuotaApi.query).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  await mount();
  expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
  await act(async () => {
    renderer.root.findAllByType('button')[1].props.onClick();
    renderer.root.findAllByType('button')[0].props.onClick();
  });
  expect(cpaBpsQuotaApi.query).toHaveBeenCalledTimes(1);
  await act(async () => {
    finish(result);
    await Promise.resolve();
  });
  expect(textContent(renderer.root)).toContain('通过插件综合额度查询获取');
  expect(renderer.root.findAllByType('button')).toHaveLength(2);
});
