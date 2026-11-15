import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
import { useQuotaStore } from '@/stores/useQuotaStore';
vi.mock('@/services/api/cpaBpsQuota', () => ({ cpaBpsQuotaApi: { query: vi.fn() } }));
import { OAuthPage } from './OAuthPage';

vi.mock('./InternalLoginCards', () => ({ InternalLoginCards: () => null }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const { mocks } = vi.hoisted(() => ({
  mocks: {
    supportsPlugin: false,
    locationSearch: '',
    apiBase: 'http://cpa-a.local:8317',
    managementKey: 'key-a',
    startAuth: vi.fn(),
    getAuthStatus: vi.fn(),
    submitCallback: vi.fn(),
    cancelSession: vi.fn(),
    authFilesList: vi.fn(async () => ({ files: [] })),
    pluginList: vi.fn(),
    vertexImport: vi.fn(),
    showNotification: vi.fn(),
    navigate: vi.fn(),
    recordMutationMarker: vi.fn(),
    publishMutationRevision: vi.fn(),
    intervalCallbacks: [] as Array<() => void | Promise<void>>,
  },
}));

vi.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: () => undefined },
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock('react-router-dom', () => ({
  useLocation: () => ({ hash: '', search: mocks.locationSearch }),
  useNavigate: () => mocks.navigate,
}));

vi.mock('@/stores', () => {
  const readAuthState = () => ({
    apiBase: mocks.apiBase,
    managementKey: mocks.managementKey,
    connectionStatus: 'connected' as const,
    supportsPlugin: mocks.supportsPlugin,
  });
  return {
    publishAccountCredentialMutationRevision: mocks.publishMutationRevision,
    useAuthStore: Object.assign(
      (selector: (state: Record<string, unknown>) => unknown) => selector(readAuthState()),
      { getState: readAuthState }
    ),
    useNotificationStore: (
      selector?: (state: { showNotification: typeof mocks.showNotification }) => unknown
    ) => {
      const state = { showNotification: mocks.showNotification };
      return selector ? selector(state) : state;
    },
    useThemeStore: (selector: (state: { resolvedTheme: 'light' }) => unknown) =>
      selector({ resolvedTheme: 'light' }),
  };
});

vi.mock('@/services/api', () => ({
  oauthApi: {
    startAuth: mocks.startAuth,
    getAuthStatus: mocks.getAuthStatus,
    submitCallback: mocks.submitCallback,
    cancelSession: mocks.cancelSession,
  },
  authFilesApi: {
    list: mocks.authFilesList,
  },
  pluginsApi: {
    list: mocks.pluginList,
  },
}));

vi.mock('@/services/api/vertex', () => ({
  vertexApi: {
    importCredential: mocks.vertexImport,
  },
}));

vi.mock('@/features/monitoring/codexInspection', () => ({
  createCodexInspectionConnectionFingerprint: (apiBase: string, managementKey: string) =>
    apiBase && managementKey ? `${apiBase}:${managementKey}` : null,
}));

vi.mock('@/features/accounts/model/accountCredentialMutationMarker', async (original) => ({
  ...(await original<typeof import('@/features/accounts/model/accountCredentialMutationMarker')>()),
  recordAccountCredentialMutationMarker: mocks.recordMutationMarker,
}));

vi.mock('@/utils/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}));

vi.mock('@/features/plugins/pluginResources', () => ({
  getPluginTitle: (plugin: { id: string }) => plugin.id,
  resolvePluginAssetURL: () => '',
}));

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, reject, resolve };
};

const textContent = (node: ReactTestInstance): string =>
  node.children.map((child) => (typeof child === 'string' ? child : textContent(child))).join('');

const treeText = (renderer: ReactTestRenderer): string =>
  renderer.root.children
    .map((child) => (typeof child === 'string' ? child : textContent(child)))
    .join('');

const findButton = (renderer: ReactTestRenderer, text: string): ReactTestInstance => {
  const button = renderer.root
    .findAllByType('button')
    .find((candidate) => textContent(candidate) === text);
  if (!button) throw new Error(`Button not found: ${text}`);
  return button;
};

const renderOAuthPage = async (): Promise<ReactTestRenderer> => {
  let renderer!: ReactTestRenderer;
  await act(async () => {
    renderer = create(<OAuthPage />);
    await Promise.resolve();
  });
  mountedRenderers.add(renderer);
  return renderer;
};

const mountedRenderers = new Set<ReactTestRenderer>();

const startCodexAuth = (renderer: ReactTestRenderer): Promise<void> => {
  let promise!: Promise<void>;
  act(() => {
    promise = Promise.resolve(
      findButton(renderer, 'auth_login.codex_oauth_button').props.onClick()
    );
  });
  return promise;
};

const startDevinAuth = (renderer: ReactTestRenderer): Promise<void> => {
  let promise!: Promise<void>;
  act(() => {
    promise = Promise.resolve(
      findButton(renderer, 'auth_login.devin_oauth_button').props.onClick()
    );
  });
  return promise;
};

describe('OAuthPage connection lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(cpaBpsQuotaApi.query).mockReset();
    useQuotaStore.getState().clearQuotaCache();
    mocks.startAuth.mockReset();
    mocks.getAuthStatus.mockReset();
    mocks.submitCallback.mockReset();
    mocks.cancelSession.mockReset();
    mocks.supportsPlugin = false;
    mocks.locationSearch = '';
    mocks.pluginList.mockReset().mockResolvedValue({ plugins: [] });
    mocks.apiBase = 'http://cpa-a.local:8317';
    mocks.managementKey = 'key-a';
    mocks.intervalCallbacks = [];
    mocks.authFilesList.mockReset();
    mocks.authFilesList.mockResolvedValue({ files: [] });
    vi.stubGlobal('window', {
      requestAnimationFrame: vi.fn(() => 1),
      cancelAnimationFrame: vi.fn(),
      setInterval: vi.fn((callback: () => void | Promise<void>) => {
        mocks.intervalCallbacks.push(callback);
        return mocks.intervalCallbacks.length;
      }),
      clearInterval: vi.fn(),
      setTimeout: vi.fn(() => 100),
      clearTimeout: vi.fn(),
      open: vi.fn(),
    });
  });

  afterEach(async () => {
    await act(async () => {
      mountedRenderers.forEach((renderer) => renderer.unmount());
    });
    mountedRenderers.clear();
    vi.unstubAllGlobals();
  });

  it('统一BPS入口等待插件加载后定位卡片，不自动创建授权或查询额度', async () => {
    mocks.locationSearch = '?provider=cpa-bps';
    mocks.supportsPlugin = true;
    const pending = deferred<{
      plugins: Array<{ id: string; oauthProvider: string; supportsOAuth: boolean }>;
    }>();
    mocks.pluginList.mockReturnValue(pending.promise);
    const focus = vi.fn();
    const scrollIntoView = vi.fn();
    let visible = false;
    vi.stubGlobal('document', {
      getElementById: vi.fn((id) =>
        visible && id === 'oauth-provider-cpa-bps' ? { focus, scrollIntoView } : null
      ),
    });
    vi.mocked(window.requestAnimationFrame).mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    const renderer = await renderOAuthPage();
    expect(treeText(renderer)).toContain('正在读取 BPS 插件授权入口');
    expect(focus).not.toHaveBeenCalled();
    await act(async () => {
      visible = true;
      pending.resolve({
        plugins: [{ id: 'cpa-bps', oauthProvider: 'cpa-bps', supportsOAuth: true }],
      });
    });
    expect(treeText(renderer)).toContain('BPS 插件授权已统一到下方卡片');
    expect(renderer.root.findByProps({ id: 'oauth-provider-cpa-bps' }).props.tabIndex).toBe(-1);
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
    expect(mocks.startAuth).not.toHaveBeenCalled();
    expect(mocks.authFilesList).not.toHaveBeenCalled();
    expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
  });

  it('统一BPS入口缺少可用插件时给出明确说明而不启动登录', async () => {
    mocks.locationSearch = '?provider=cpa-bps';
    mocks.supportsPlugin = true;
    const renderer = await renderOAuthPage();
    expect(treeText(renderer)).toContain('当前连接没有可用的 BPS 插件授权入口');
    expect(renderer.root.findAllByType('a').some((node) => node.props.href === '#/plugins')).toBe(
      true
    );
    expect(mocks.startAuth).not.toHaveBeenCalled();
    expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
  });

  it('统一BPS入口读取失败显示失败状态，不误报插件未安装', async () => {
    mocks.locationSearch = '?provider=cpa-bps';
    mocks.supportsPlugin = true;
    mocks.pluginList.mockRejectedValue(new Error('offline'));
    const renderer = await renderOAuthPage();
    expect(treeText(renderer)).toContain('读取 BPS 插件授权入口失败');
    expect(mocks.startAuth).not.toHaveBeenCalled();
  });

  it('已在OAuth页时进入BPS定位链接会重新装载插件而不丢失入口', async () => {
    mocks.supportsPlugin = true;
    mocks.pluginList.mockResolvedValue({
      plugins: [{ id: 'cpa-bps', oauthProvider: 'cpa-bps', supportsOAuth: true }],
    });
    const renderer = await renderOAuthPage();
    mocks.locationSearch = '?provider=cpa-bps';
    await act(async () => {
      renderer.update(<OAuthPage />);
    });
    expect(treeText(renderer)).toContain('BPS 插件授权已统一到下方卡片');
    expect(renderer.root.findByProps({ id: 'oauth-provider-cpa-bps' })).toBeTruthy();
    expect(mocks.startAuth).not.toHaveBeenCalled();
  });

  it('同页定位刷新以BPS替换等数量插件后仍聚焦，不自动授权或查询额度', async () => {
    mocks.supportsPlugin = true;
    mocks.pluginList.mockResolvedValueOnce({
      plugins: [{ id: 'other-plugin', oauthProvider: 'other-plugin', supportsOAuth: true }],
    });
    const pending = deferred<{
      plugins: Array<{ id: string; oauthProvider: string; supportsOAuth: boolean }>;
    }>();
    mocks.pluginList.mockReturnValueOnce(pending.promise);
    const focus = vi.fn();
    const scrollIntoView = vi.fn();
    let visible = false;
    vi.stubGlobal('document', {
      getElementById: vi.fn((id) =>
        visible && id === 'oauth-provider-cpa-bps' ? { focus, scrollIntoView } : null
      ),
    });
    vi.mocked(window.requestAnimationFrame).mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    const renderer = await renderOAuthPage();
    const providerCount = () =>
      renderer.root.findAll(
        (node) => typeof node.props.id === 'string' && node.props.id.startsWith('oauth-provider-')
      ).length;
    const initialCount = providerCount();
    expect(renderer.root.findByProps({ id: 'oauth-provider-other-plugin' })).toBeTruthy();
    mocks.locationSearch = '?provider=cpa-bps';
    await act(async () => {
      renderer.update(<OAuthPage />);
    });
    expect(mocks.pluginList).toHaveBeenCalledTimes(2);
    expect(focus).not.toHaveBeenCalled();
    await act(async () => {
      visible = true;
      pending.resolve({
        plugins: [{ id: 'cpa-bps', oauthProvider: 'cpa-bps', supportsOAuth: true }],
      });
    });
    expect(providerCount()).toBe(initialCount);
    expect(renderer.root.findByProps({ id: 'oauth-provider-cpa-bps' })).toBeTruthy();
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
    expect(mocks.startAuth).not.toHaveBeenCalled();
    expect(mocks.authFilesList).not.toHaveBeenCalled();
    expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
  });

  async function startPlugin(renderer: ReactTestRenderer) {
    await act(async () => {
      await findButton(renderer, 'auth_login.plugin_oauth_button').props.onClick();
    });
  }
  function enableBPSPlugin() {
    mocks.supportsPlugin = true;
    mocks.pluginList.mockResolvedValue({ plugins: [{ id: 'cpa-bps', oauthProvider: 'cpa-bps', supportsOAuth: true }] });
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/bps', state: 'bps-state' });
    mocks.getAuthStatus.mockResolvedValue({ status: 'wait' });
  }

  it('cpa-bps严格导入后只查询新增身份一次，查询失败仍成功', async () => {
    enableBPSPlugin();
    const old = { name: 'old.json', id: 'old-id', provider: 'cpa-bps', auth_index: 'old-index' };
    mocks.authFilesList.mockResolvedValue({ files: [old] } as never);
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    const imported = { name: 'new.json', id: 'new-id', provider: 'cpa-bps', auth_index: 'new-index' };
    mocks.authFilesList.mockResolvedValue({ files: [old, imported] } as never);
    vi.mocked(cpaBpsQuotaApi.query).mockRejectedValue(new Error('quota unavailable'));
    mocks.getAuthStatus.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => { await mocks.intervalCallbacks[0](); await Promise.resolve(); });
    expect(cpaBpsQuotaApi.query).toHaveBeenCalledTimes(1);
    expect(cpaBpsQuotaApi.query).toHaveBeenCalledWith('new-index', { apiBase: mocks.apiBase, managementKey: mocks.managementKey }, 'new-id');
    await act(async () => { await mocks.intervalCallbacks[0](); renderer.update(<OAuthPage />); });
    expect(cpaBpsQuotaApi.query).toHaveBeenCalledTimes(1);
    expect(treeText(renderer)).toContain('auth_login.plugin_oauth_status_success');
    expect(mocks.showNotification).not.toHaveBeenCalledWith(expect.anything(), 'error');
    expect(Object.values(useQuotaStore.getState().cpaBpsQuota)[0].status).toBe('error');
  });
  it('cpa-bps多份新增身份不猜测、不批量查询', async () => {
    enableBPSPlugin();
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    mocks.authFilesList.mockResolvedValue({ files: [1, 2].map(id => ({ name: `${id}.json`, id: String(id), provider: 'cpa-bps', auth_index: String(id) })) } as never);
    mocks.getAuthStatus.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(cpaBpsQuotaApi.query).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).toHaveBeenCalledTimes(1);
  });

  it('cpa-bps提供完整回调入口且ACK不成功、不重复提交', async () => {
    enableBPSPlugin();
    mocks.submitCallback.mockResolvedValue({ status: 'ok' });
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    const input = renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder');
    expect(input).toBeDefined();
    act(() => input!.props.onChange({ target: { value: 'https://bps.openai.com/auth/callback?code=once&state=bps-state' } }));
    const click = findButton(renderer, 'auth_login.oauth_callback_button').props.onClick;
    await act(async () => { await Promise.all([click(), click()]); });
    expect(mocks.submitCallback).toHaveBeenCalledTimes(1);
    expect(mocks.submitCallback).toHaveBeenCalledWith('cpa-bps', 'https://bps.openai.com/auth/callback?code=once&state=bps-state', { apiBase: mocks.apiBase, managementKey: mocks.managementKey });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(input!.props.value).toBe('');
    expect(input!.props.disabled).toBe(true);
    expect(mocks.showNotification).not.toHaveBeenCalledWith('auth_login.oauth_callback_success', 'success');
  });
  it.each([{}, {saved:true,adopted:false}, {saved:false,adopted:true}, {saved:'true',adopted:true}, {saved:true,adopted:'true'}])('cpa-bps保存注册需严格布尔确认 %j', async fields => {
    enableBPSPlugin();
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    mocks.getAuthStatus.mockResolvedValueOnce({status:'ok',...fields});
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).not.toHaveBeenCalled();
    mocks.getAuthStatus.mockResolvedValueOnce({status:'ok',saved:true,adopted:true});
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(mocks.recordMutationMarker).toHaveBeenCalledTimes(1);
    expect(mocks.publishMutationRevision).toHaveBeenCalledWith(expect.objectContaining({ provider: 'cpa-bps', kind: 'oauth' }));
  });

  it('cpa-bps回调400可纠正，回调不确定则只读查询', async () => {
    enableBPSPlugin();
    mocks.submitCallback.mockRejectedValueOnce(Object.assign(new Error('invalid'), {status:400})).mockRejectedValueOnce(Object.assign(new Error('unknown'), {status:502}));
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    const input=renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    for (const code of ['invalid','once']) {
      expect(input.props.disabled).toBe(false);
      act(() => input.props.onChange({target:{value:'https://bps.openai.com/auth/callback?code='+code+'&state=bps-state'}}));
      await act(async () => {await findButton(renderer,'auth_login.oauth_callback_button').props.onClick();});
    }
    expect(input.props.disabled).toBe(true);
    await act(async () => {await findButton(renderer,'auth_login.oauth_callback_button').props.onClick();await mocks.intervalCallbacks[0]();});
    expect(mocks.submitCallback).toHaveBeenCalledTimes(2);
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
  });
  it('cpa-bps切连接后迟到回调ACK不查询或污染新连接', async () => {
    enableBPSPlugin();
    const pending=deferred<{status:'ok'}>();
    mocks.submitCallback.mockReturnValueOnce(pending.promise);
    const renderer=await renderOAuthPage();
    await startPlugin(renderer);
    const input=renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    act(() => input.props.onChange({target:{value:'https://bps.openai.com/auth/callback?code=once&state=bps-state'}}));
    let callback!:Promise<void>;
    act(() => {callback=findButton(renderer,'auth_login.oauth_callback_button').props.onClick();});
    mocks.apiBase='https://new.invalid';
    await act(async () => {renderer.update(<OAuthPage />);pending.resolve({status:'ok'});await callback;});
    expect(mocks.getAuthStatus).not.toHaveBeenCalled();
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith('auth_login.oauth_callback_success','success');
  });
  it('Codex与cpa-bps回调锁互不阻塞', async () => {
    enableBPSPlugin();
    mocks.startAuth.mockImplementation(async provider => ({url:'https://auth.example/'+provider,state:provider+'-state'}));
    mocks.submitCallback.mockResolvedValue({status:'ok'});
    const renderer=await renderOAuthPage();
    await startPlugin(renderer);
    await act(async () => {await startCodexAuth(renderer);});
    for (const provider of ['codex','cpa-bps']) {
      const card=renderer.root.findByProps({id:'oauth-provider-'+provider});
      const input=card.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
      act(() => input.props.onChange({target:{value:'https://callback.invalid/?code='+provider+'&state='+provider+'-state'}}));
      await act(async () => {await card.findAllByType('button').find(button=>textContent(button)==='auth_login.oauth_callback_button')!.props.onClick();});
    }
    expect(mocks.submitCallback).toHaveBeenCalledTimes(2);
    expect(mocks.submitCallback.mock.calls.map(call=>call[0])).toEqual(['codex','cpa-bps']);
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
  });

  it('cpa-bps切连接后迟到保存回执不污染新连接', async () => {
    enableBPSPlugin();
    const pending=deferred<{status:'ok';saved:true;adopted:true}>();
    mocks.getAuthStatus.mockReturnValueOnce(pending.promise);
    const renderer = await renderOAuthPage();
    await startPlugin(renderer);
    let polling!: void|Promise<void>;
    act(() => { polling=mocks.intervalCallbacks[0](); });
    mocks.apiBase='https://new.invalid';
    await act(async () => {
      renderer.update(<OAuthPage />);
      pending.resolve({status:'ok',saved:true,adopted:true});
      await polling;
    });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith('auth_login.plugin_oauth_status_success','success');
  });

  it.each([
    {}, { saved: true, adopted: false }, { saved: false, adopted: true },
    { saved: 'true', adopted: true }, { saved: true, adopted: 'true' },
  ])('Codex严格核保存注册，不把不确定结果当成功 %j', async (fields) => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.getAuthStatus.mockResolvedValueOnce({ status: 'ok', ...fields });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); });
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith('auth_login.codex_oauth_status_success', 'success');
    expect(treeText(renderer)).toContain(fields.saved === true && fields.adopted === false ? '已保存，等待注册确认' : '授权结果未确认');
    mocks.getAuthStatus.mockResolvedValueOnce({ status: 'ok', saved: true, adopted: true });
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(mocks.recordMutationMarker).toHaveBeenCalledTimes(1);
  });

  it('Codex回调回执不算导入成功，连续点击与已接收回调不重复提交', async () => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockResolvedValue({ status: 'ok' });
    mocks.getAuthStatus.mockResolvedValue({ status: 'wait' });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); });
    const input = renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    act(() => input.props.onChange({ target: { value: 'http://localhost:1455/auth/callback?code=once&state=state-a' } }));
    const click = findButton(renderer, 'auth_login.oauth_callback_button').props.onClick;
    await act(async () => { await Promise.all([click(), click()]); });
    await act(async () => { await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); });
    expect(mocks.submitCallback).toHaveBeenCalledTimes(1);
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(input.props.value).toBe('');
    expect(findButton(renderer, 'auth_login.oauth_callback_button').props.disabled).toBe(true);
    expect(mocks.showNotification).not.toHaveBeenCalledWith('auth_login.oauth_callback_success', 'success');
  });

  it.each([undefined, 500, 409])('Codex回调结果未知时只读查询，不重交授权码 %s', async (status) => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockRejectedValue(Object.assign(new Error('请求未确认'), { status }));
    mocks.getAuthStatus.mockResolvedValue({ status: 'wait' });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); });
    const input = renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    act(() => input.props.onChange({ target: { value: 'http://localhost:1455/auth/callback?code=once&state=state-a' } }));
    await act(async () => { await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); });
    await act(async () => { await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); await mocks.intervalCallbacks[0](); });
    expect(mocks.submitCallback).toHaveBeenCalledTimes(1);
    expect(mocks.getAuthStatus).toHaveBeenCalled();
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(treeText(renderer)).toContain('授权结果未确认');
    expect(input.props.value).toBe('');
    expect(input.props.disabled).toBe(true);
  });

  it('Codex输入被400拒绝后允许纠正，已接收后保持只读查询', async () => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockRejectedValueOnce(Object.assign(new Error('输入无效'), { status: 400 })).mockResolvedValue({ status: 'ok' });
    mocks.getAuthStatus.mockResolvedValue({ status: 'wait' });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); });
    const input = renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    for (const code of ['invalid', 'corrected']) {
      expect(input.props.disabled).toBe(false);
      act(() => input.props.onChange({ target: { value: `http://localhost:1455/auth/callback?code=${code}&state=state-a` } }));
      await act(async () => { await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); });
    }
    expect(mocks.submitCallback).toHaveBeenCalledTimes(2);
    expect(input.props.disabled).toBe(true);
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
  });

  it('Codex已保存后遇到流程过期，保留注册未确认且不重启授权', async () => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.getAuthStatus.mockResolvedValueOnce({ status: 'ok', saved: true, adopted: false }).mockResolvedValueOnce({ status: 'error', error: 'expired' });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); await mocks.intervalCallbacks[0](); });
    await act(async () => { await mocks.intervalCallbacks[0](); });
    expect(treeText(renderer)).toContain('已保存，注册结果未确认');
    expect(mocks.startAuth).toHaveBeenCalledTimes(1);
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.publishMutationRevision).not.toHaveBeenCalled();
  });

  it('Codex旧回调400晚到不解锁同连接的新授权', async () => {
    const oldCallback = deferred<{ status: 'ok' }>();
    mocks.startAuth.mockResolvedValueOnce({ url: 'https://auth.example/old', state: 'old' }).mockResolvedValueOnce({ url: 'https://auth.example/new', state: 'new' });
    mocks.submitCallback.mockReturnValueOnce(oldCallback.promise).mockResolvedValue({ status: 'ok' });
    mocks.getAuthStatus.mockResolvedValueOnce({ status: 'error', error: 'expired' }).mockResolvedValue({ status: 'wait' });
    const renderer = await renderOAuthPage();
    await act(async () => { await startCodexAuth(renderer); });
    const input = renderer.root.findAllByType('input').find(node => node.props.placeholder === 'auth_login.oauth_callback_placeholder')!;
    act(() => input.props.onChange({ target: { value: 'http://localhost:1455/auth/callback?code=old&state=old' } }));
    let pending!: Promise<void>;
    await act(async () => { pending = findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); await Promise.resolve(); });
    await act(async () => { await mocks.intervalCallbacks[0](); });
    await act(async () => { await startCodexAuth(renderer); });
    act(() => input.props.onChange({ target: { value: 'http://localhost:1455/auth/callback?code=new&state=new' } }));
    await act(async () => { await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick(); });
    await act(async () => { oldCallback.reject(Object.assign(new Error('旧输入无效'), { status: 400 })); await pending; });
    expect(input.props.disabled).toBe(true);
    expect(treeText(renderer)).not.toContain('旧输入无效');
    expect(treeText(renderer)).toContain('回调已接收，等待保存和注册确认');
    expect(mocks.submitCallback).toHaveBeenCalledTimes(2);
  });

  it('ignores a late auth-link response after the CPA connection changes', async () => {
    const oldRequest = deferred<{ url: string; state: string }>();
    const newRequest = deferred<{ url: string; state: string }>();
    mocks.startAuth.mockImplementation((_provider: string, scope: { apiBase: string }) =>
      scope.apiBase === 'http://cpa-a.local:8317' ? oldRequest.promise : newRequest.promise
    );
    const renderer = await renderOAuthPage();

    const oldAuthPromise = startCodexAuth(renderer);
    await act(async () => {
      await Promise.resolve();
    });
    expect(mocks.startAuth).toHaveBeenCalledWith('codex', {
      apiBase: 'http://cpa-a.local:8317',
      managementKey: 'key-a',
    });

    mocks.apiBase = 'http://cpa-b.local:8317';
    mocks.managementKey = 'key-b';
    await act(async () => {
      renderer.update(<OAuthPage />);
    });
    const newAuthPromise = startCodexAuth(renderer);
    await act(async () => {
      await Promise.resolve();
    });

    await act(async () => {
      oldRequest.resolve({ url: 'https://auth.example/old', state: 'old-state' });
      await oldAuthPromise;
    });
    expect(treeText(renderer)).not.toContain('https://auth.example/old');
    expect(mocks.intervalCallbacks).toHaveLength(0);

    await act(async () => {
      newRequest.resolve({ url: 'https://auth.example/new', state: 'new-state' });
      await newAuthPromise;
    });
    expect(treeText(renderer)).toContain('https://auth.example/new');
    expect(mocks.intervalCallbacks).toHaveLength(1);
  });

  it('ignores a late polling success after the CPA connection changes', async () => {
    const polling = deferred<{ status: 'ok' }>();
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.getAuthStatus.mockReturnValue(polling.promise);
    const renderer = await renderOAuthPage();
    const authPromise = startCodexAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    let pollingPromise!: Promise<void>;
    await act(async () => {
      pollingPromise = Promise.resolve(mocks.intervalCallbacks[0]?.());
      await Promise.resolve();
    });
    expect(mocks.getAuthStatus).toHaveBeenCalledWith('state-a', {
      apiBase: 'http://cpa-a.local:8317',
      managementKey: 'key-a',
    });

    mocks.apiBase = 'http://cpa-b.local:8317';
    mocks.managementKey = 'key-b';
    await act(async () => {
      renderer.update(<OAuthPage />);
      polling.resolve({ status: 'ok' });
      await pollingPromise;
    });

    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith(
      'auth_login.codex_oauth_status_success',
      'success'
    );
  });

  it('ignores a late callback response after the CPA connection changes', async () => {
    const callback = deferred<{ status: 'ok' }>();
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockReturnValue(callback.promise);
    const renderer = await renderOAuthPage();
    const authPromise = startCodexAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    const callbackInput = renderer.root
      .findAllByType('input')
      .find((input) => input.props.placeholder === 'auth_login.oauth_callback_placeholder');
    if (!callbackInput) throw new Error('Callback input not found');
    act(() => callbackInput.props.onChange({ target: { value: 'http://callback?code=1' } }));
    let callbackPromise!: Promise<void>;
    await act(async () => {
      callbackPromise = Promise.resolve(
        findButton(renderer, 'auth_login.oauth_callback_button').props.onClick()
      );
      await Promise.resolve();
    });
    expect(mocks.submitCallback).toHaveBeenCalledWith('codex', 'http://callback?code=1', {
      apiBase: 'http://cpa-a.local:8317',
      managementKey: 'key-a',
    });

    mocks.apiBase = 'http://cpa-b.local:8317';
    mocks.managementKey = 'key-b';
    await act(async () => {
      renderer.update(<OAuthPage />);
      callback.resolve({ status: 'ok' });
      await callbackPromise;
    });

    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith(
      'auth_login.oauth_callback_success',
      'success'
    );
  });

  it('records one scoped mutation marker when callback success races with polling success', async () => {
    const polling = deferred<{ status: 'ok'; saved: true; adopted: true }>();
    const callbackStatus = deferred<{ status: 'ok'; saved: true; adopted: true }>();
    const callback = deferred<{ status: 'ok' }>();
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.getAuthStatus
      .mockReturnValueOnce(polling.promise)
      .mockReturnValueOnce(callbackStatus.promise);
    mocks.submitCallback.mockReturnValue(callback.promise);
    const renderer = await renderOAuthPage();
    const authPromise = startCodexAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    let pollingPromise!: Promise<void>;
    await act(async () => {
      pollingPromise = Promise.resolve(mocks.intervalCallbacks[0]?.());
      await Promise.resolve();
    });
    const callbackInput = renderer.root
      .findAllByType('input')
      .find((input) => input.props.placeholder === 'auth_login.oauth_callback_placeholder');
    if (!callbackInput) throw new Error('Callback input not found');
    act(() => callbackInput.props.onChange({ target: { value: 'http://callback?code=1' } }));
    let callbackPromise!: Promise<void>;
    await act(async () => {
      callbackPromise = Promise.resolve(
        findButton(renderer, 'auth_login.oauth_callback_button').props.onClick()
      );
      await Promise.resolve();
    });

    await act(async () => {
      callback.resolve({ status: 'ok' });
      await Promise.resolve();
    });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    await act(async () => {
      callbackStatus.resolve({ status: 'ok', saved: true, adopted: true });
      await callbackPromise;
    });
    expect(mocks.recordMutationMarker).toHaveBeenCalledTimes(1);
    expect(mocks.recordMutationMarker).toHaveBeenCalledWith({
      connectionFingerprint: 'http://cpa-a.local:8317:key-a',
      provider: 'codex',
      baseline: { provider: 'codex', credentials: [] },
      requireObservedMutation: true,
    });
    expect(mocks.showNotification).not.toHaveBeenCalledWith(
      'auth_login.oauth_callback_success',
      'success'
    );

    await act(async () => {
      polling.resolve({ status: 'ok', saved: true, adopted: true });
      await pollingPromise;
    });
    expect(mocks.recordMutationMarker).toHaveBeenCalledTimes(1);
    expect(
      mocks.showNotification.mock.calls.filter(
        ([message, level]) =>
          message === 'auth_login.codex_oauth_status_success' && level === 'success'
      )
    ).toHaveLength(1);
  });

  it('keeps the provider attempt pending after an accepted callback', async () => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockResolvedValue({ status: 'ok' });
    mocks.getAuthStatus.mockResolvedValue({ status: 'wait' });
    const renderer = await renderOAuthPage();
    const authPromise = startCodexAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    const callbackInput = renderer.root
      .findAllByType('input')
      .find((input) => input.props.placeholder === 'auth_login.oauth_callback_placeholder');
    if (!callbackInput) throw new Error('Callback input not found');
    act(() => callbackInput.props.onChange({ target: { value: 'http://callback?code=1' } }));
    await act(async () => {
      await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick();
    });

    expect(mocks.getAuthStatus).toHaveBeenCalledWith('state-a', {
      apiBase: 'http://cpa-a.local:8317',
      managementKey: 'key-a',
    });
    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(treeText(renderer)).toContain('回调已接收，等待保存和注册确认');
    expect(treeText(renderer)).toContain('auth_login.codex_oauth_status_waiting');
  });

  it('confirms an already-completed callback only through provider status', async () => {
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/codex', state: 'state-a' });
    mocks.submitCallback.mockRejectedValue(
      Object.assign(new Error('oauth flow is already completed'), { status: 409 })
    );
    mocks.getAuthStatus.mockResolvedValue({ status: 'ok', saved: true, adopted: true });
    const renderer = await renderOAuthPage();
    const authPromise = startCodexAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    const callbackInput = renderer.root
      .findAllByType('input')
      .find((input) => input.props.placeholder === 'auth_login.oauth_callback_placeholder');
    if (!callbackInput) throw new Error('Callback input not found');
    act(() => callbackInput.props.onChange({ target: { value: 'http://callback?code=1' } }));
    await act(async () => {
      await findButton(renderer, 'auth_login.oauth_callback_button').props.onClick();
    });

    expect(mocks.getAuthStatus).toHaveBeenCalledWith('state-a', {
      apiBase: 'http://cpa-a.local:8317',
      managementKey: 'key-a',
    });
    expect(mocks.recordMutationMarker).toHaveBeenCalledTimes(1);
  });

  it('ignores a late Devin polling response after the CPA connection changes', async () => {
    const polling = deferred<{ status: 'ok' }>();
    mocks.startAuth.mockResolvedValue({ url: 'https://auth.example/devin', state: 'state-devin-a' });
    mocks.getAuthStatus.mockReturnValue(polling.promise);
    const renderer = await renderOAuthPage();
    const authPromise = startDevinAuth(renderer);
    await act(async () => {
      await authPromise;
    });

    let pollingPromise!: Promise<void>;
    await act(async () => {
      pollingPromise = Promise.resolve(mocks.intervalCallbacks[0]?.());
      await Promise.resolve();
    });

    mocks.apiBase = 'http://cpa-b.local:8317';
    mocks.managementKey = 'key-b';
    await act(async () => {
      renderer.update(<OAuthPage />);
      polling.resolve({ status: 'ok' });
      await pollingPromise;
    });

    expect(mocks.recordMutationMarker).not.toHaveBeenCalled();
    expect(mocks.showNotification).not.toHaveBeenCalledWith(
      'auth_login.devin_oauth_status_success',
      'success'
    );
  });
});
