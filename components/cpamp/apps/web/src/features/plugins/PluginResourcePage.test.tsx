import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PluginResourcePage } from './PluginResourcePage';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  bridge: vi.fn(),
  t: (key: string) => key,
  params: { pluginId: 'cpa-bps', menuIndex: '0' },
}));
vi.mock('react-router-dom', () => ({
  useParams: () => mocks.params,
  Navigate: ({ to, replace }: { to: string; replace: boolean }) => (
    <span data-navigate={to} data-replace={replace} />
  ),
}));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock('@/hooks/useHeaderRefresh', () => ({ useHeaderRefresh: () => undefined }));
vi.mock('@/services/api', () => ({ pluginsApi: { list: mocks.list } }));
vi.mock('@/stores', () => ({
  useAuthStore: (select: (state: Record<string, unknown>) => unknown) =>
    select({
      apiBase: 'https://panel.invalid',
      managementKey: 'TEST_ONLY',
      connectionStatus: 'connected',
      supportsPlugin: true,
    }),
  useThemeStore: (select: (state: Record<string, unknown>) => unknown) =>
    select({ resolvedTheme: 'light' }),
}));
vi.mock('./pluginHostStyle', () => ({ createPluginHostStyleBridge: mocks.bridge }));

let renderer: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  vi.stubGlobal('window', { addEventListener: vi.fn(), removeEventListener: vi.fn() });
  mocks.params = { pluginId: 'cpa-bps', menuIndex: '0' };
  mocks.list.mockResolvedValue({
    plugins: [
      {
        id: 'cpa-bps',
        effectiveEnabled: true,
        logo: '',
        metadata: null,
        menus: [
          { path: '/v0/resource/plugins/cpa-bps/index.html', menu: 'BPS 授权', description: '' },
          { path: '/v0/resource/plugins/cpa-bps/other.html', menu: '其他', description: '' },
        ],
      },
    ],
  });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer?.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
});
describe('BPS插件授权旧深链接', () => {
  it('确认菜单为授权资源后替换路由，不加载iframe', async () => {
    await act(async () => {
      renderer = create(<PluginResourcePage />);
    });
    expect(
      renderer!.root.findByProps({ 'data-navigate': '/oauth?provider=cpa-bps' }).props[
        'data-replace'
      ]
    ).toBe(true);
    expect(renderer!.root.findAllByType('iframe')).toHaveLength(0);
    expect(mocks.bridge).not.toHaveBeenCalled();
  });
  it('同插件其他资源继续显示iframe', async () => {
    mocks.params = { pluginId: 'cpa-bps', menuIndex: '1' };
    await act(async () => {
      renderer = create(<PluginResourcePage />);
    });
    expect(renderer!.root.findByType('iframe').props.src).toBe(
      'https://panel.invalid/v0/resource/plugins/cpa-bps/other.html'
    );
    expect(
      renderer!.root.findAllByProps({ 'data-navigate': '/oauth?provider=cpa-bps' })
    ).toHaveLength(0);
  });
});
