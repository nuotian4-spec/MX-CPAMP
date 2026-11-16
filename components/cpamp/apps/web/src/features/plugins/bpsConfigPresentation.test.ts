import { describe, expect, it } from 'vitest';
import { bpsConfigPresentation } from './bpsConfigPresentation';
import { buildPluginConfigPatch, createPluginConfigDraft } from './pluginConfigDraft';
import { pluginMaintenanceLabel } from './pluginMaintenance';
import type { UpdateOperation } from '@/types/pluginImport';

const field = (type: string, name = 'session_proxy_pool') => ({
  name,
  type,
  enumValues: [],
  description: '',
});
const t = (key: string) => key;

describe('BPS 配置类型与维护状态', () => {
  it('仅对 BPS 精确字段且声明 string 使用多行编辑', () => {
    expect(bpsConfigPresentation('cpa-bps', field('string')).multiline).toBe(true);
    expect(bpsConfigPresentation('cpa-bps', field('array')).multiline).toBe(false);
    expect(bpsConfigPresentation('cpa-kilo', field('string')).multiline).toBe(false);
    expect(
      bpsConfigPresentation('cpa-bps', field('boolean', 'session_proxy_pool_enabled')).label
    ).toBe('启用会话代理池');
  });
  it('多行 string 原样 PATCH；未修改与未知顶层键不发出', () => {
    const fields = [field('string'), field('boolean', 'session_proxy_pool_enabled')];
    const draft = createPluginConfigDraft(
      fields,
      { session_proxy_pool: 'old', untouched: { child: true } },
      true
    );
    expect(buildPluginConfigPatch(draft, fields, t, 'cpa-bps').patch).toEqual({});
    draft.values.session_proxy_pool = ' http://first.invalid:80\r\nsocks5://second.invalid:90\n';
    draft.touched.add('session_proxy_pool');
    expect(buildPluginConfigPatch(draft, fields, t, 'cpa-bps').patch).toEqual({
      session_proxy_pool: draft.values.session_proxy_pool,
    });
    draft.values.session_proxy_pool = '';
    expect(buildPluginConfigPatch(draft, fields, t, 'cpa-bps').patch).toEqual({
      session_proxy_pool: null,
    });
  });
  it('实际 array / object 类型仍以 JSON 保存，不套 string 适配', () => {
    for (const [type, value] of [
      ['array', ['x']],
      ['object', { value: true }],
    ] as const) {
      const fields = [field(type)];
      const draft = createPluginConfigDraft(fields, { session_proxy_pool: value }, true);
      draft.touched.add('session_proxy_pool');
      expect(buildPluginConfigPatch(draft, fields, t, 'cpa-bps').patch).toEqual({
        session_proxy_pool: value,
      });
    }
  });
  it('未知类型明确报错而非降级字符串，未触碰仍不写回', () => {
    const fields = [field('list')];
    const draft = createPluginConfigDraft(fields, { session_proxy_pool: ['x'] }, true);
    expect(buildPluginConfigPatch(draft, fields, t, 'cpa-bps').patch).toEqual({});
    draft.touched.add('session_proxy_pool');
    const result = buildPluginConfigPatch(draft, fields, t, 'cpa-bps');
    expect(result.patch).toEqual({});
    expect(result.errors.session_proxy_pool).toContain('尚不支持');
  });
  it.each(['recovery_required', 'failed', 'awaiting_restart', 'installing'] as const)(
    '许可未知不覆盖 %s',
    (status) => {
      expect(pluginMaintenanceLabel({ status } as UpdateOperation, true)).not.toContain(
        '许可尚未验证'
      );
      if (status === 'recovery_required')
        expect(pluginMaintenanceLabel({ status } as UpdateOperation, true)).toBe(
          '任务中断，需核实恢复'
        );
    }
  );
});
