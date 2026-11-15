import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, expect, it, vi } from 'vitest';
import { BpsResetCreditsCard } from './BpsResetCreditsCard';
import { bpsResetCreditsApi } from '@/services/api/bpsResetCredits';
import { useQuotaStore } from '@/stores/useQuotaStore';
vi.mock('@/services/api/bpsResetCredits', () => ({ bpsResetCreditsApi: { query: vi.fn() } }));
const file = { name: 'native.json', id: 'one', auth_index: 'one-index', provider: 'bps' };
const scope = { apiBase: 'https://fixture.invalid', managementKey: 'key' };
const result = {
  provider: 'bps' as const,
  auth_id: 'one',
  auth_index: 'one-index',
  source: 'chatgpt_account_reset_credits' as const,
  query_mode: 'direct' as const,
  queried_at: '2026-11-15T00:00:00Z',
  count: 0,
  status: 'known' as const,
  error: null,
};
let renderer: ReactTestRenderer;
afterEach(async () => {
  if (renderer) await act(async () => renderer.unmount());
  useQuotaStore.getState().clearQuotaCache();
  vi.clearAllMocks();
});
it('B新增:原生次数按钮按需GET、真实0；换连接不显示旧次数', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.mocked(bpsResetCreditsApi.query).mockResolvedValue(result);
  await act(async () => {
    renderer = create(<BpsResetCreditsCard file={file} scope={scope} connectionRef="ref" />);
  });
  expect(bpsResetCreditsApi.query).not.toHaveBeenCalled();
  expect(JSON.stringify(renderer.toJSON())).toContain('未查询');
  await act(async () => {
    await renderer.root.findByType('button').props.onClick();
  });
  expect(bpsResetCreditsApi.query).toHaveBeenCalledWith('one-index', 'one', scope, 'ref');
  expect(JSON.stringify(renderer.toJSON())).toContain('0 次');
  expect(renderer.root.findAllByType('button')).toHaveLength(1);
  await act(async () => {
    renderer.update(<BpsResetCreditsCard file={file} scope={scope} connectionRef="new-ref" />);
  });
  expect(JSON.stringify(renderer.toJSON())).toContain('未查询');
  expect(JSON.stringify(renderer.toJSON())).not.toContain('0 次');
});
