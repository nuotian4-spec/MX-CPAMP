import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { QuotaEstimateSummary } from './QuotaEstimateSummary';
import { estimateWindowUsage } from '@/features/accounts/model/estimateWindowUsage';
let renderer: ReactTestRenderer;
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
});
describe('独立预计额度与重置动作', () => {
  it('列表摘要只有两行与详情入口，不触发查询或消费', async () => {
    const open = vi.fn(),
      query = vi.fn(),
      reset = vi.fn(),
      consume = vi.fn();
    await act(async () => {
      renderer = create(
        <QuotaEstimateSummary
          compact
          windows={[]}
          resetCount={null}
          onOpen={open}
          onQueryQuota={query}
          onQueryReset={reset}
          onUseReset={consume}
        />
      );
    });
    expect(renderer.root.findAllByType(Button)).toHaveLength(0);
    expect(JSON.stringify(renderer.toJSON())).toContain('额度详情');
    await act(async () =>
      renderer.root.findByType('button').props.onClick({ stopPropagation() {} })
    );
    expect(open).toHaveBeenCalledOnce();
    expect(query).not.toHaveBeenCalled();
    expect(reset).not.toHaveBeenCalled();
    expect(consume).not.toHaveBeenCalled();
  });
  it('15美元/25%复用既有公式，窗口分列，上一周期标明参考', async () => {
    const estimate = estimateWindowUsage({
      usedPercent: 25,
      current: { cost: 15, requests: 10, tokens: 100 },
      previous: null,
    });
    await act(async () => {
      renderer = create(
        <QuotaEstimateSummary
          windows={[
            { key: '5h', label: '5小时', cost: estimate!.cost, previous: false },
            { key: '7d', label: '7天', cost: 80, previous: true },
          ]}
        />
      );
    });
    const text = JSON.stringify(renderer.toJSON());
    expect(text).toContain('≈ $60.00');
    expect(text).toContain('上周期参考 $80.00');
    expect(text).not.toContain('$140');
  });
  it.each([null, 0])('次数%s仍可独立查询，两种查询均不消耗重置', async (count) => {
    const quota = vi.fn().mockResolvedValue(undefined),
      reset = vi.fn().mockResolvedValue(undefined),
      use = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      renderer = create(
        <QuotaEstimateSummary
          windows={[]}
          resetCount={count}
          onQueryQuota={quota}
          onQueryReset={reset}
          onUseReset={use}
        />
      );
    });
    const button = (text: string) =>
      renderer.root.findAllByType(Button).find((item) => item.props.children === text)!;
    expect(button('确认使用重置').props.disabled).toBe(true);
    await act(async () => {
      button('查询额度').props.onClick();
    });
    await act(async () => {
      button('查询重置次数').props.onClick();
    });
    expect(quota).toHaveBeenCalledTimes(1);
    expect(reset).toHaveBeenCalledTimes(1);
    expect(use).not.toHaveBeenCalled();
    expect(JSON.stringify(renderer.toJSON())).toContain(count === null ? '未知' : '可用重置次数');
  });
  it('使用只进入外部确认链路，查询失败保留观测值', async () => {
    const use = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      renderer = create(
        <QuotaEstimateSummary
          windows={[{ key: '5h', label: '5小时', cost: 60, previous: false }]}
          resetCount={2}
          onUseReset={use}
          onQueryReset={async () => {
            throw new Error();
          }}
        />
      );
    });
    await act(async () =>
      renderer.root
        .findAllByType(Button)
        .find((item) => item.props.children === '确认使用重置')!
        .props.onClick()
    );
    expect(use).toHaveBeenCalledTimes(1);
    await act(async () =>
      renderer.root
        .findAllByType(Button)
        .find((item) => item.props.children === '查询重置次数')!
        .props.onClick()
    );
    expect(JSON.stringify(renderer.toJSON())).toContain('≈ $60.00');
    expect(JSON.stringify(renderer.toJSON())).toContain('保留上次观测');
  });
  it.each([2, null])('来源失败投影保留次数%s与原观测时间', async (count) => {
    const props = {
      windows: [],
      resetCount: count,
      resetObservedAtMs: count === null ? null : 1700000000000,
    };
    await act(async () => {
      renderer = create(<QuotaEstimateSummary {...props} />);
    });
    const previous = JSON.stringify(renderer.toJSON());
    await act(async () =>
      renderer.update(<QuotaEstimateSummary {...props} resetError="查询失败" />)
    );
    const after = JSON.stringify(renderer.toJSON());
    expect(after).toContain('重置次数查询失败');
    expect(after).toContain(count === null ? '次数仍为未知' : '保留上次观测');
    if (count !== null) {
      expect(previous).toContain(new Date(1700000000000).toLocaleString());
      expect(after).toContain(new Date(1700000000000).toLocaleString());
    }
    await act(async () =>
      renderer.update(
        <QuotaEstimateSummary
          {...props}
          resetError="查询失败"
          onQueryReset={async () => {
            throw new Error('查询失败');
          }}
        />
      )
    );
    await act(async () =>
      renderer.root
        .findAllByType(Button)
        .find((item) => item.props.children === '查询重置次数')!
        .props.onClick()
    );
    expect(renderer.root.findAllByProps({ role: 'alert' })).toHaveLength(1);
  });
});
