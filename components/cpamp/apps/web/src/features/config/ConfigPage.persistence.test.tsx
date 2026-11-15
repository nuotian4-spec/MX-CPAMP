import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import type { ReactNode } from 'react';
import zhCN from '@/i18n/locales/zh-CN.json';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import type { ApiKeyMutation } from '@/components/config/ApiKeysCardEditor';
import type { ManagerConfigResponse } from '@/services/api/usageService';
import type { VisualConfigValues } from '@/types/visualConfig';
import { parse as parseYaml, stringify as stringifyYaml } from 'yaml';

declare const process: { env: Record<string, string | undefined> };

vi.mock('react-dom', () => ({
  createPortal: (children: ReactNode) => children,
}));

const mocks = vi.hoisted(() => ({
  fetchConfigYaml: vi.fn(),
  fetchSnapshot: vi.fn(),
  realKeyEditor: false,
  connection: {
    apiBase: 'http://panel.local',
    managementKey: 'management-key',
    connectionStatus: 'connected',
  },
  saveConfigYaml: vi.fn(),
  apiKeysList: vi.fn(),
  apiKeysReplace: vi.fn(),
  apiKeysReplaceValue: vi.fn(),
  apiKeysDeleteValue: vi.fn(),
  apiKeyMutationErrors: [] as unknown[],
  getInfo: vi.fn(),
  showNotification: vi.fn(),
  showConfirmation: vi.fn(),
  commitApiKeysText: vi.fn(),
  loadVisualValuesFromYaml: vi.fn(),
  applyVisualChangesToYaml: vi.fn(),
  setVisualValues: vi.fn(),
  clearCache: vi.fn(),
  fetchGlobalConfig: vi.fn(),
  setUsageServiceConfig: vi.fn(),
  getManagerConfig: vi.fn(),
  saveManagerConfig: vi.fn(),
  reloadPage: vi.fn(),
  capturedApiKeyOperationStart: null as (() => void) | null,
  capturedApiKeyOperationEnd: null as (() => void) | null,
  translate: (key: string) => key,
  useRealVisualConfig: false,
  visualState: {
    apiKeysText: 'sk-old',
    dirty: false,
  },
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mocks.translate }),
}));

vi.mock('@/components/common/PageTransitionLayer', () => ({
  usePageTransitionLayer: () => null,
}));

vi.mock('@/hooks/useMediaQuery', () => ({
  useMediaQuery: () => false,
}));

vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? (
      <div>
        {children}
        {footer}
      </div>
    ) : null,
}));
vi.mock('@/hooks/usePanelFeatureAvailability', () => ({
  usePanelFeatureAvailability: () => ({
    checking: false,
    panelHostConfirmed: true,
    panelHostMode: 'cpa_embedded',
    managerServiceAvailable: false,
    managerServiceBase: '',
  }),
}));

vi.mock('@/components/config/VisualConfigEditor', async () => {
  const { ApiKeysCardEditor } = await import('@/components/config/ApiKeysCardEditor');
  return {
    VisualConfigEditor: ({
      values,
      onChange,
      onPersistApiKeyMutation,
      onApiKeyOperationStart,
      onApiKeyOperationEnd,
      onRefreshApiKeys,
    }: {
      values: VisualConfigValues;
      onChange: (values: Partial<VisualConfigValues>) => void;
      onPersistApiKeyMutation: (mutation: ApiKeyMutation) => Promise<string[]>;
      onApiKeyOperationStart: () => void;
      onApiKeyOperationEnd: () => void;
      onRefreshApiKeys: () => Promise<string[]>;
    }) => {
      mocks.capturedApiKeyOperationStart = onApiKeyOperationStart;
      mocks.capturedApiKeyOperationEnd = onApiKeyOperationEnd;
      const runMutation = async (mutation: ApiKeyMutation) => {
        try {
          onApiKeyOperationStart();
          await onPersistApiKeyMutation(mutation);
        } catch (error) {
          // The real editor renders mutation errors. This harness only observes the page contract.
          mocks.apiKeyMutationErrors.push(error);
        } finally {
          onApiKeyOperationEnd();
        }
      };

      if (mocks.realKeyEditor)
        return (
          <div>
            <ApiKeysCardEditor
              value={values.apiKeysText}
              scopes={values.clientKeyScopes}
              onPersistApiKeyMutation={onPersistApiKeyMutation}
              onRefreshApiKeys={onRefreshApiKeys}
              onApiKeyOperationStart={onApiKeyOperationStart}
              onApiKeyOperationEnd={onApiKeyOperationEnd}
            />
            <button data-test="enable-debug" onClick={() => onChange({ debug: true })} />
            <button
              type="button"
              data-test="enable-commercial"
              onClick={() => onChange({ commercialMode: true })}
            />
          </div>
        );
      return (
        <div data-test="visual-editor">
          <output data-test="api-keys">{values.apiKeysText}</output>
          <button
            type="button"
            data-test="enable-debug"
            onClick={() => onChange({ debug: true })}
          />
          <button
            type="button"
            data-test="enable-commercial"
            onClick={() => onChange({ commercialMode: true })}
          />
          <button
            type="button"
            data-test="create-key"
            onClick={() => runMutation({ type: 'create', apiKey: 'sk-new' })}
          />
          <button
            type="button"
            data-test="replace-key"
            onClick={() =>
              runMutation({ type: 'replace', oldApiKey: 'sk-old', newApiKey: 'sk-new' })
            }
          />
          <button
            type="button"
            data-test="delete-key"
            onClick={() => runMutation({ type: 'delete', apiKey: 'sk-old' })}
          />
        </div>
      );
    },
  };
});

vi.mock('./components/ManagerConfigPanel', () => ({
  ManagerConfigPanel: ({
    managerSaving,
    onCollectorModeChange,
    onCPABaseInputChange,
    onCPAManagementKeyInputChange,
  }: {
    managerSaving: boolean;
    onCollectorModeChange: (value: string) => void;
    onCPABaseInputChange: (value: string) => void;
    onCPAManagementKeyInputChange: (value: string) => void;
  }) => (
    <div data-test="manager-panel">
      <button
        type="button"
        data-test="manager-dirty"
        disabled={managerSaving}
        onClick={() => onCollectorModeChange('http')}
      />
      <button
        type="button"
        data-test="manager-change-cpa"
        disabled={managerSaving}
        onClick={() => onCPABaseInputChange('http://cpa-next.local:8317')}
      />
      <button
        type="button"
        data-test="manager-change-key"
        disabled={managerSaving}
        onClick={() => onCPAManagementKeyInputChange('next-management-key')}
      />
    </div>
  ),
}));

vi.mock('@/components/config/DiffModal', () => ({
  DiffModal: ({
    open,
    onConfirm,
    onCancel,
  }: {
    open: boolean;
    onConfirm: () => Promise<void>;
    onCancel: () => void;
  }) =>
    open ? (
      <div>
        <button type="button" data-test="confirm-save" onClick={onConfirm} />
        <button data-test="cancel-save" onClick={onCancel} />
      </div>
    ) : null,
}));

vi.mock('@/components/config/ConfigSourceEditor', () => ({
  default: ({
    value,
    onChange,
    editable,
  }: {
    value: string;
    onChange: (value: string) => void;
    editable: boolean;
  }) => (
    <textarea
      data-test="source-editor"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      readOnly={!editable}
    />
  ),
}));

vi.mock('@/components/ui/SegmentedTabs', () => ({
  SegmentedTabs: ({
    items,
    activeTab,
    onChange,
  }: {
    items: ReadonlyArray<{ id: string; label: ReactNode; disabled?: boolean }>;
    activeTab: string;
    onChange?: (tab: string) => void;
  }) => (
    <div data-test="tabs">
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          data-tab={item.id}
          data-active={item.id === activeTab}
          disabled={item.disabled}
          onClick={() => onChange?.(item.id)}
        >
          {item.label}
        </button>
      ))}
    </div>
  ),
}));

vi.mock('@/stores', () => ({
  useAuthStore: (
    selector: (state: {
      connectionStatus: string;
      managementKey: string;
      serverVersion: string;
      serverCommit: string;
    }) => unknown
  ) =>
    selector({
      ...mocks.connection,
      connectionStatus: mocks.connection.connectionStatus,
      managementKey: mocks.connection.managementKey,
      serverVersion: 'v8.0.11',
      serverCommit: 'e2bff01',
    }),
  useNotificationStore: (
    selector: (state: {
      showNotification: typeof mocks.showNotification;
      showConfirmation: typeof mocks.showConfirmation;
    }) => unknown
  ) =>
    selector({
      showNotification: mocks.showNotification,
      showConfirmation: mocks.showConfirmation,
    }),
  useThemeStore: (selector: (state: { resolvedTheme: string }) => unknown) =>
    selector({ resolvedTheme: 'light' }),
  useConfigStore: {
    getState: () => ({
      clearCache: mocks.clearCache,
      fetchConfig: mocks.fetchGlobalConfig,
    }),
  },
  useUsageServiceStore: (
    selector: (state: { setUsageServiceConfig: typeof mocks.setUsageServiceConfig }) => unknown
  ) => selector({ setUsageServiceConfig: mocks.setUsageServiceConfig }),
}));

vi.mock('@/hooks/useVisualConfig', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/useVisualConfig')>();
  return {
    useVisualConfig: (runtime?: Parameters<typeof actual.useVisualConfig>[0]) => {
      const realConfig = actual.useVisualConfig(runtime);
      if (mocks.useRealVisualConfig) return realConfig;
      return {
        visualValues: {
          apiKeysText: mocks.visualState.apiKeysText,
          redisUsageQueueRetentionSeconds: '60',
        },
        visualDirty: mocks.visualState.dirty,
        visualParseError: null,
        visualValidationErrors: {},
        visualHasPayloadValidationErrors: false,
        loadVisualValuesFromYaml: mocks.loadVisualValuesFromYaml,
        applyVisualChangesToYaml: mocks.applyVisualChangesToYaml,
        setVisualValues: mocks.setVisualValues,
        commitApiKeysText: mocks.commitApiKeysText,
      };
    },
  };
});

vi.mock('@/services/api/configFile', () => ({
  configFileApi: {
    fetchConfigYaml: mocks.fetchConfigYaml,
    fetchSnapshot: mocks.fetchSnapshot,
    saveConfigYaml: mocks.saveConfigYaml,
  },
}));

vi.mock('@/services/api/apiKeys', () => ({
  apiKeysApi: {
    list: mocks.apiKeysList,
    replace: mocks.apiKeysReplace,
    replaceValue: mocks.apiKeysReplaceValue,
    deleteValue: mocks.apiKeysDeleteValue,
  },
}));

vi.mock('@/services/api/usageService', () => ({
  getUsageServiceErrorCode: () => '',
  isUsageServiceId: (service?: string) => service === 'test-manager',
  normalizeUsageServiceBase: (value: string) => value,
  usageServiceApi: {
    getInfo: mocks.getInfo,
    getManagerConfig: mocks.getManagerConfig,
    saveManagerConfig: mocks.saveManagerConfig,
  },
}));

vi.mock('@/utils/connection', async (original) => ({
  ...(await original<typeof import('@/utils/connection')>()),
  detectApiBaseFromLocation: () => 'http://panel.local',
}));

const { ConfigPage } = await import('./ConfigPage');

const INITIAL_YAML = 'api-keys:\n  - sk-old\n';
const MANAGER_CONFIG_RESPONSE: ManagerConfigResponse = {
  config: {
    cpaConnection: {
      cpaBaseUrl: 'http://cpa.local:8317',
      managementKeyConfigured: true,
    },
    collector: {
      enabled: true,
      collectorMode: 'auto',
      queue: 'usage',
      popSide: 'right',
      batchSize: 100,
      pollIntervalMs: 500,
      queryLimit: 50000,
    },
    externalUsageService: {
      enabled: false,
      serviceBase: '',
    },
  },
  source: 'db',
  cpaUsage: {
    usageStatisticsEnabled: true,
    redisUsageQueueRetentionSeconds: 60,
  },
};

let renderer: ReactTestRenderer | null = null;
const originalLocalStorage = globalThis.localStorage;
const originalDocument = globalThis.document;

type ConfirmationOptions = {
  onConfirm: () => void | Promise<void>;
};

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T | PromiseLike<T>) => void;
  reject: (reason?: unknown) => void;
};

const createDeferred = <T,>(): Deferred<T> => {
  let resolve!: Deferred<T>['resolve'];
  let reject!: Deferred<T>['reject'];
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve;
    reject = promiseReject;
  });
  return { promise, resolve, reject };
};

const flush = async () => {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
};

const mountPage = async () => {
  await act(async () => {
    renderer = create(<ConfigPage />);
  });
  await flush();
  return renderer as ReactTestRenderer;
};

const click = async (testId: string) => {
  const target = renderer?.root.findByProps({ 'data-test': testId });
  if (!target) throw new Error(`Test target not found: ${testId}`);
  await act(async () => {
    if (testId.endsWith('-key')) void target.props.onClick();
    else await target.props.onClick();
  });
  await flush();
};

const clickTab = async (tab: string) => {
  const target = renderer?.root.findByProps({ 'data-tab': tab });
  if (!target) throw new Error(`Tab not found: ${tab}`);
  await act(async () => {
    await target.props.onClick();
  });
  await flush();
};

const startPendingSave = async () => {
  const target = renderer?.root.findByProps({ 'aria-label': 'config_management.save' });
  if (!target) throw new Error('Save button not found');
  let pending!: Promise<unknown>;
  await act(async () => {
    pending = target.props.onClick() as Promise<unknown>;
    await Promise.resolve();
  });
  await flush();
  return { pending };
};

const clickSave = async () => {
  const target = renderer?.root.findByProps({ 'aria-label': 'config_management.save' });
  if (!target) throw new Error('Save button not found');
  await act(async () => {
    await target.props.onClick();
  });
  await flush();
};

const getPendingConfirmation = (): ConfirmationOptions => {
  const call = mocks.showConfirmation.mock.calls[mocks.showConfirmation.mock.calls.length - 1];
  const options = call?.[0] as ConfirmationOptions | undefined;
  if (!options) throw new Error('Confirmation not found');
  return options;
};

const confirmPending = async () => {
  const confirmation = getPendingConfirmation();
  await act(async () => {
    await confirmation.onConfirm();
  });
  await flush();
};

const configureManagerMode = () => {
  mocks.getInfo.mockResolvedValue({ service: 'test-manager' });
  mocks.getManagerConfig.mockResolvedValue(MANAGER_CONFIG_RESPONSE);
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.useRealVisualConfig = false;
  mocks.realKeyEditor = false;
  mocks.translate = (key: string) => key;
  mocks.connection = {
    apiBase: 'http://panel.local',
    managementKey: 'management-key',
    connectionStatus: 'connected',
  };
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: () => null,
      setItem: vi.fn(),
    },
  });
  mocks.fetchConfigYaml.mockResolvedValue(INITIAL_YAML);
  mocks.fetchSnapshot.mockImplementation(async () => ({
    yaml: await mocks.fetchConfigYaml(),
    etag: '"test-etag"',
    connectionRef: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  }));
  mocks.saveConfigYaml.mockResolvedValue(undefined);
  mocks.apiKeysList.mockResolvedValue([]);
  mocks.apiKeysReplace.mockResolvedValue(undefined);
  mocks.apiKeysReplaceValue.mockResolvedValue(undefined);
  mocks.apiKeysDeleteValue.mockResolvedValue(undefined);
  mocks.apiKeyMutationErrors.length = 0;
  mocks.getInfo.mockRejectedValue(new Error('not a Manager Server panel'));
  mocks.getManagerConfig.mockResolvedValue(MANAGER_CONFIG_RESPONSE);
  mocks.saveManagerConfig.mockResolvedValue(MANAGER_CONFIG_RESPONSE);
  mocks.capturedApiKeyOperationStart = null;
  mocks.capturedApiKeyOperationEnd = null;
  mocks.loadVisualValuesFromYaml.mockReturnValue({ ok: true });
  mocks.applyVisualChangesToYaml.mockImplementation((yaml: string) => yaml);
  mocks.commitApiKeysText.mockImplementation((apiKeysText: string) => {
    mocks.visualState.apiKeysText = apiKeysText;
    mocks.visualState.dirty = false;
  });
  mocks.visualState.apiKeysText = 'sk-old';
  mocks.visualState.dirty = false;
  vi.stubGlobal('window', Object.assign(new EventTarget(), { location: { reload: mocks.reloadPage } }));
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: { body: {} },
  });
});

afterEach(() => {
  if (renderer) {
    act(() => renderer?.unmount());
    renderer = null;
  }
  vi.unstubAllGlobals();
  if (originalLocalStorage === undefined) {
    Reflect.deleteProperty(globalThis, 'localStorage');
  } else {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: originalLocalStorage,
    });
  }
  if (originalDocument === undefined) {
    Reflect.deleteProperty(globalThis, 'document');
  } else {
    Object.defineProperty(globalThis, 'document', {
      configurable: true,
      value: originalDocument,
    });
  }
});

describe('ConfigPage CPA v8 visual persistence', () => {
  it('resolves merged commercial mode when deciding the restart warning', async () => {
    mocks.useRealVisualConfig = true;
    let serverYaml = [
      'defaults: &server',
      '  commercial-mode: false',
      'server:',
      '  <<: *server',
      '',
    ].join('\n');
    mocks.fetchConfigYaml.mockImplementation(async () => serverYaml);
    mocks.saveConfigYaml.mockImplementation(async (yaml: string) => {
      serverYaml = yaml;
    });

    await mountPage();
    await click('enable-commercial');
    await clickSave();
    await click('confirm-save');

    const effective = parseYaml(serverYaml, { merge: true }) as {
      server?: Record<string, unknown>;
    };
    expect(effective.server?.['commercial-mode']).toBe(true);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'notification.commercial_mode_restart_required',
      'warning'
    );
  });

  it('saves commercial mode to server.commercial-mode and emits the restart warning', async () => {
    mocks.useRealVisualConfig = true;
    let serverConfig: {
      'config-version': number;
      server: { 'commercial-mode': boolean };
    } = {
      'config-version': 8,
      server: { 'commercial-mode': false },
    };
    mocks.fetchConfigYaml.mockImplementation(async () => stringifyYaml(serverConfig));
    mocks.saveConfigYaml.mockImplementation(async (yaml: string) => {
      serverConfig = parseYaml(yaml) as typeof serverConfig;
    });

    await mountPage();
    await click('enable-commercial');
    await clickSave();
    await click('confirm-save');

    expect(serverConfig.server['commercial-mode']).toBe(true);
    expect(serverConfig).not.toHaveProperty('commercial-mode');
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'notification.commercial_mode_restart_required',
      'warning'
    );
  });
});

describe('ConfigPage Manager/API-key operation lock', () => {
  it('blocks switching CPA bases while Visual config has unsaved changes', async () => {
    configureManagerMode();
    mocks.visualState.dirty = true;
    await mountPage();
    await clickTab('manager');

    await click('manager-change-cpa');
    await clickSave();

    expect(mocks.saveManagerConfig).not.toHaveBeenCalled();
    expect(mocks.reloadPage).not.toHaveBeenCalled();
    expect(mocks.showConfirmation).not.toHaveBeenCalled();
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'config_management.manager.cpa_switch_unsaved_config',
      'warning'
    );
  });

  it('blocks switching CPA bases while Source has unsaved changes', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('source');

    const sourceEditor = renderer?.root.findByProps({ 'data-test': 'source-editor' });
    if (!sourceEditor) throw new Error('Source editor not found');
    act(() => {
      sourceEditor.props.onChange({ target: { value: `${INITIAL_YAML}# unsaved draft\n` } });
    });

    await clickTab('manager');
    await click('manager-change-cpa');
    await clickSave();

    expect(mocks.saveManagerConfig).not.toHaveBeenCalled();
    expect(mocks.reloadPage).not.toHaveBeenCalled();
    expect(mocks.showConfirmation).not.toHaveBeenCalled();
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'config_management.manager.cpa_switch_unsaved_config',
      'warning'
    );
  });

  it('reloads once after a successful CPA base switch', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-change-cpa');

    const saveDeferred = createDeferred<ManagerConfigResponse>();
    mocks.saveManagerConfig.mockReturnValueOnce(saveDeferred.promise);
    await clickSave();

    expect(mocks.showConfirmation).toHaveBeenCalledTimes(1);
    expect(mocks.saveManagerConfig).not.toHaveBeenCalled();
    const confirmation = getPendingConfirmation();
    let savePromise!: Promise<void>;
    await act(async () => {
      savePromise = confirmation.onConfirm() as Promise<void>;
      await Promise.resolve();
    });
    await flush();

    expect(mocks.saveManagerConfig).toHaveBeenCalledTimes(1);
    expect(mocks.reloadPage).not.toHaveBeenCalled();

    saveDeferred.resolve(MANAGER_CONFIG_RESPONSE);
    await act(async () => {
      await savePromise;
    });
    await flush();

    expect(mocks.reloadPage).toHaveBeenCalledTimes(1);
  });

  it('reloads after a CPA base save request fails', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-change-cpa');
    mocks.saveManagerConfig.mockRejectedValueOnce(new Error('CPA switch failed'));
    await clickSave();

    const confirmation = getPendingConfirmation();
    await act(async () => {
      await confirmation.onConfirm();
    });
    await flush();

    expect(mocks.reloadPage).toHaveBeenCalledTimes(1);
  });

  it('does not reload for a Management Key-only save', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-change-key');
    await clickSave();
    await confirmPending();

    expect(mocks.saveManagerConfig).toHaveBeenCalledTimes(1);
    expect(mocks.reloadPage).not.toHaveBeenCalled();
  });

  it('does not reload when a Management Key-only save request fails', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-change-key');
    mocks.saveManagerConfig.mockRejectedValueOnce(new Error('Management Key rejected'));
    await clickSave();

    const confirmation = getPendingConfirmation();
    await act(async () => {
      await expect(confirmation.onConfirm()).rejects.toThrow('Management Key rejected');
    });
    await flush();

    expect(mocks.reloadPage).not.toHaveBeenCalled();
  });

  it('blocks API-key operation and tab changes while Manager Save is pending', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-dirty');

    const saveDeferred = createDeferred<ManagerConfigResponse>();
    mocks.saveManagerConfig.mockReturnValueOnce(saveDeferred.promise);
    const { pending: savePromise } = await startPendingSave();

    expect(renderer?.root.findByProps({ 'data-tab': 'visual' }).props.disabled).toBe(true);
    expect(renderer?.root.findByProps({ 'data-tab': 'source' }).props.disabled).toBe(true);
    expect(renderer?.root.findByProps({ 'data-tab': 'manager' }).props.disabled).toBe(true);
    expect(renderer?.root.findByProps({ 'data-tab': 'manager' }).props['data-active']).toBe(true);

    expect(mocks.capturedApiKeyOperationStart).toEqual(expect.any(Function));
    expect(() => mocks.capturedApiKeyOperationStart!()).toThrow();
    expect(mocks.apiKeysList).not.toHaveBeenCalled();
    expect(mocks.apiKeysReplace).not.toHaveBeenCalled();
    expect(mocks.apiKeysReplaceValue).not.toHaveBeenCalled();
    expect(mocks.apiKeysDeleteValue).not.toHaveBeenCalled();

    saveDeferred.resolve(MANAGER_CONFIG_RESPONSE);
    await act(async () => {
      await savePromise;
    });
    await flush();

    expect(renderer?.root.findByProps({ 'data-tab': 'visual' }).props.disabled).toBe(false);
  });

  it('blocks Manager Save while an API-key operation is in flight', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-dirty');

    expect(mocks.capturedApiKeyOperationStart).toEqual(expect.any(Function));
    act(() => {
      mocks.capturedApiKeyOperationStart!();
    });
    await flush();

    await clickSave();

    expect(mocks.saveManagerConfig).not.toHaveBeenCalled();

    expect(mocks.capturedApiKeyOperationEnd).toEqual(expect.any(Function));
    act(() => {
      mocks.capturedApiKeyOperationEnd!();
    });
    await flush();
  });

  it('releases the Manager Save lock after a successful request', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-dirty');

    const saveDeferred = createDeferred<ManagerConfigResponse>();
    mocks.saveManagerConfig.mockReturnValueOnce(saveDeferred.promise);
    const { pending: savePromise } = await startPendingSave();

    saveDeferred.resolve(MANAGER_CONFIG_RESPONSE);
    await act(async () => {
      await savePromise;
    });
    await flush();

    await clickTab('visual');
    await click('create-key');

    expect(mocks.fetchSnapshot).toHaveBeenCalled();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(1);
    await click('cancel-save');
    expect(mocks.apiKeysReplace).not.toHaveBeenCalled();
    expect(mocks.reloadPage).not.toHaveBeenCalled();
  });

  it('releases the Manager Save lock after a failed request', async () => {
    configureManagerMode();
    await mountPage();
    await clickTab('manager');
    await click('manager-dirty');

    const saveDeferred = createDeferred<ManagerConfigResponse>();
    mocks.saveManagerConfig.mockReturnValueOnce(saveDeferred.promise);
    const { pending: savePromise } = await startPendingSave();

    saveDeferred.reject(new Error('Manager save failed'));
    await act(async () => {
      await savePromise;
    });
    await flush();

    await clickTab('visual');
    await click('create-key');

    expect(mocks.fetchSnapshot).toHaveBeenCalled();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(1);
    await click('cancel-save');
    expect(mocks.apiKeysReplace).not.toHaveBeenCalled();
  });
});

describe('三通道Key真实组件与配置事务', () => {
  const yaml =
    'config-version: 8\naccess:\n  api-keys: [sk-old, sk-kilo, sk-codex, sk-bps]\n  client-key-scopes:\n    - {api-key: sk-kilo, channel: kilo}\n    - {api-key: sk-codex, channel: codex}\n    - {api-key: sk-bps, channel: bps}\napi-keys: {codex: [{api-key: upstream-only}]}\ncustom: {keep: true}\n';
  let disk: string;
  const editorButton = (label: string, index = 0) => {
    const matches = renderer!.root
      .findAllByType(Button)
      .filter((node) =>
        label === 'config_management.visual.common.save'
          ? [
              'config_management.visual.common.add',
              'config_management.visual.common.update',
            ].includes(node.props.children)
          : node.props.children === label
      );
    if (!matches[index]) throw new Error(`找不到按钮 ${label}`);
    return matches[index];
  };
  const press = async (label: string, index = 0) => {
    await act(async () => {
      void editorButton(label, index).props.onClick();
    });
    await flush();
  };
  const fill = (key: string, channel?: string) => {
    act(() => {
      renderer!.root
        .findByProps({ placeholder: 'config_management.visual.api_keys.input_placeholder' })
        .props.onChange({ target: { value: key } });
      if (channel !== undefined)
        renderer!.root.findByType('select').props.onChange({ target: { value: channel } });
    });
  };
  const prepare = async (initialYaml = yaml) => {
    mocks.realKeyEditor = true;
    mocks.useRealVisualConfig = true;
    disk = initialYaml;
    mocks.fetchConfigYaml.mockImplementation(async () => disk);
    mocks.fetchSnapshot.mockImplementation(async () => ({
      yaml: disk,
      etag: '"version-1"',
      connectionRef: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    }));
    mocks.saveConfigYaml.mockImplementation(async (content: string) => {
      disk = content;
    });
    await mountPage();
  };
  const startAdd = async (key = 'sk-added', channel = 'kilo') => {
    await press('config_management.visual.api_keys.add');
    fill(key, channel);
    await press('config_management.visual.common.save');
  };
  it.each(['kilo', 'codex', 'bps'])(
    '创建%s仅在diff确认后原子保存并保留旧Key及未知字段',
    async (channel) => {
      await prepare();
      await startAdd('sk-added', channel);
      expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
      expect(parseYaml(disk)).toEqual(parseYaml(yaml));
      await click('confirm-save');
      const result = parseYaml(disk);
      expect(result.access['api-keys']).toEqual([
        'sk-old',
        'sk-kilo',
        'sk-codex',
        'sk-bps',
        'sk-added',
      ]);
      expect(result.access['client-key-scopes']).toContainEqual({ 'api-key': 'sk-added', channel });
      expect(
        result.access['client-key-scopes'].some(
          (scope: { 'api-key': string }) => scope['api-key'] === 'sk-old'
        )
      ).toBe(false);
      expect(result['api-keys']).toEqual(parseYaml(yaml)['api-keys']);
      expect(result.custom).toEqual({ keep: true });
      expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(1);
      expect(mocks.saveConfigYaml.mock.calls[0][1]).toMatchObject({
        scope: { apiBase: mocks.connection.apiBase, managementKey: mocks.connection.managementKey },
        etag: '"version-1"',
        connectionRef: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      });
      expect(mocks.apiKeysReplace).not.toHaveBeenCalled();
      expect(mocks.apiKeysReplaceValue).not.toHaveBeenCalled();
    }
  );
  it('Key事务同时提交visual草稿后刷新全局缓存并提示商用模式重启', async () => {
    const initialYaml = yaml.replace(
      'api-keys: {codex: [{api-key: upstream-only}]}',
      'server:\n  commercial-mode: false\napi-keys: {codex: [{api-key: upstream-only}]}'
    );
    await prepare(initialYaml);
    await click('enable-commercial');
    await startAdd('sk-commercial', 'kilo');

    expect(parseYaml(disk).server['commercial-mode']).toBe(false);
    expect(mocks.clearCache).not.toHaveBeenCalled();
    await click('confirm-save');

    const saved = parseYaml(disk);
    expect(saved.server['commercial-mode']).toBe(true);
    expect(saved.access['api-keys']).toContain('sk-commercial');
    expect(saved.access['client-key-scopes']).toContainEqual({
      'api-key': 'sk-commercial',
      channel: 'kilo',
    });
    expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(1);
    expect(mocks.clearCache).toHaveBeenCalledTimes(1);
    expect(mocks.fetchGlobalConfig).toHaveBeenCalledWith(undefined, true);
    expect(mocks.showNotification).toHaveBeenCalledWith(
      'notification.commercial_mode_restart_required',
      'warning'
    );
  });
  it('替换保归属，显式改绑及解除归属，删除撤引用', async () => {
    await prepare();
    await press('config_management.visual.common.edit', 1);
    fill('sk-replaced');
    await press('config_management.visual.common.save');
    await click('confirm-save');
    expect(parseYaml(disk).access['client-key-scopes']).toContainEqual({
      'api-key': 'sk-replaced',
      channel: 'kilo',
    });
    await press('config_management.visual.common.edit', 1);
    fill('sk-replaced', 'bps');
    await press('config_management.visual.common.save');
    await click('confirm-save');
    expect(parseYaml(disk).access['client-key-scopes']).toContainEqual({
      'api-key': 'sk-replaced',
      channel: 'bps',
    });
    await press('config_management.visual.common.edit', 1);
    fill('sk-replaced', '');
    await press('config_management.visual.common.save');
    await click('confirm-save');
    expect(
      parseYaml(disk).access['client-key-scopes'].some(
        (scope: { 'api-key': string }) => scope['api-key'] === 'sk-replaced'
      )
    ).toBe(false);
    await press('config_management.visual.common.delete', 2);
    await act(async () => {
      void getPendingConfirmation().onConfirm();
    });
    await flush();
    await click('confirm-save');
    expect(parseYaml(disk).access['api-keys']).not.toContain('sk-codex');
    expect(parseYaml(disk).access['client-key-scopes']).toEqual([
      { 'api-key': 'sk-bps', channel: 'bps' },
    ]);
  });
  it('取消和保存失败保留原状态，不写入别名或部分列表', async () => {
    await prepare();
    await startAdd();
    await click('cancel-save');
    expect(disk).toBe(yaml);
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
    await press('config_management.visual.common.save');
    mocks.saveConfigYaml.mockRejectedValueOnce(new Error('保存失败'));
    await click('confirm-save');
    expect(disk).toBe(yaml);
    expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(1);
  });
  it('确认前配置漂移拒绝，连接漂移取消且不发写请求', async () => {
    await prepare();
    await startAdd();
    disk = yaml + 'changed: true\n';
    await click('confirm-save');
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
    disk = yaml;
    await press('config_management.visual.common.save');
    mocks.connection = { ...mocks.connection, apiBase: 'http://other.local' };
    await act(async () => {
      renderer!.update(<ConfigPage />);
    });
    await flush();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(0);
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
  });
  it('保存后回读失败不发布成功列表，也不重放写请求', async () => {
    await prepare();
    await startAdd();
    mocks.saveConfigYaml.mockImplementationOnce(async (content: string) => {
      disk = content;
      mocks.fetchSnapshot.mockRejectedValue(new Error('回读失败'));
    });
    await click('confirm-save');
    expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(1);
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(0);
    expect(
      renderer!.root.findByProps({
        placeholder: 'config_management.visual.api_keys.input_placeholder',
      }).props.value
    ).toBe('sk-added');
  });
  it('原始YAML回读归属与无关visual保存互保', async () => {
    await prepare();
    await clickTab('source');
    const changed = yaml.replace('channel: kilo', 'channel: bps');
    act(() =>
      renderer!.root
        .findByProps({ 'data-test': 'source-editor' })
        .props.onChange({ target: { value: changed } })
    );
    await clickSave();
    await click('confirm-save');
    await clickTab('visual');
    await press('config_management.visual.common.edit', 1);
    expect(renderer!.root.findByType('select').props.value).toBe('bps');
    await press('config_management.visual.common.cancel');
    await click('enable-debug');
    await clickSave();
    await click('confirm-save');
    expect(parseYaml(disk).access).toEqual(parseYaml(changed).access);
    expect(parseYaml(disk).custom).toEqual({ keep: true });
  });
  it.each([
    'access: {api-keys: [sk-old, sk-old]}',
    'access: {api-keys: [sk-old], client-key-scopes: [{api-key: sk-old, channel: other}]}',
    'access: {api-keys: [sk-old], client-key-scopes: [{api-key: missing, channel: kilo}]}',
  ])('最新配置非法时不发PUT: %s', async (invalid) => {
    await prepare();
    mocks.fetchSnapshot.mockResolvedValue({
      yaml: invalid,
      etag: '"v2"',
      connectionRef: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    });
    await startAdd();
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(0);
    expect(disk).toBe(yaml);
  });

  it.each(['etag', 'connectionRef'])('缺少%s时拒绝保护保存', async (field) => {
    await prepare();
    mocks.fetchSnapshot.mockResolvedValue({
      yaml,
      etag: '"version-1"',
      connectionRef: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      [field]: '',
    });
    await startAdd();
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(0);
  });
  it('保存已发出后回读引用改变不发布成功、不重放', async () => {
    await prepare();
    await startAdd();
    mocks.saveConfigYaml.mockImplementationOnce(async (content: string) => {
      disk = content;
      mocks.fetchSnapshot.mockResolvedValue({
        yaml: disk,
        etag: '"version-2"',
        connectionRef: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      });
    });
    await click('confirm-save');
    expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(1);
    expect(
      renderer!.root.findByProps({
        placeholder: 'config_management.visual.api_keys.input_placeholder',
      }).props.value
    ).toBe('sk-added');
    expect(JSON.stringify(renderer!.toJSON())).toContain('原目标状态待核');
    expect(mocks.showNotification.mock.calls.some((call) => call[1] === 'success')).toBe(false);
  });
  it('重复Key在真实表单拒绝，不生成差异或写请求', async () => {
    await prepare();
    await startAdd('sk-old', 'kilo');
    expect(mocks.saveConfigYaml).not.toHaveBeenCalled();
    expect(renderer!.root.findAllByProps({ 'data-test': 'confirm-save' })).toHaveLength(0);
  });

  it('实际中文标签与差异确认提示在真实Key编辑器中渲染', async () => {
    mocks.translate = (key: string) => {
      let value: unknown = zhCN;
      for (const part of key.split('.')) value = (value as Record<string, unknown> | undefined)?.[part];
      return typeof value === 'string' ? value : key;
    };
    await prepare();
    const visible = JSON.stringify(renderer!.toJSON());
    expect(visible).toContain('兼容旧规则（未绑定）');
    expect(visible).toContain('新增、修改或删除API密钥后，在“确认变更”中检查差异并确认保存。');
    expect(visible).not.toContain('会立即保存');
    await press('添加 API 密钥');
    const select = renderer!.root.findByType('select');
    expect(select.props['aria-label']).toBe('客户端通道');
    expect(select.findAllByType('option').map((option) => option.props.children)).toEqual(['兼容旧规则（未绑定）', 'Kilo', 'Codex', 'BPS']);
    expect(JSON.stringify(renderer!.toJSON())).toContain('添加或更新后确认配置差异，才会保存到CPA');
  });
  it.skipIf(!process.env.KILO_KEYS_MANAGER_URL || !process.env.KILO_KEYS_CONTROL_URL)(
    '真实组件经Manager和Core保存三通道Key',
    async () => {
      const managerURL = process.env.KILO_KEYS_MANAGER_URL!;
      const controlURL = process.env.KILO_KEYS_CONTROL_URL!;
      const managementKey = process.env.KILO_KEYS_MANAGER_KEY!;
      for (const address of [managerURL, controlURL]) {
        const target = new URL(address);
        expect(target.protocol).toBe('http:');
        expect(['127.0.0.1', '[::1]']).toContain(target.hostname);
      }
      const fixture = (await (await fetch(controlURL + '/fixture')).json()) as {
        legacy_key: string;
      };
      expect(fixture.legacy_key).toBe('m9-legacy');
      mocks.realKeyEditor = true;
      mocks.useRealVisualConfig = true;
      mocks.connection = { apiBase: managerURL, managementKey, connectionStatus: 'connected' };
      const actual = await vi.importActual<typeof import('@/services/api/configFile')>(
        '@/services/api/configFile'
      );
      const { apiClient } = await import('@/services/api/client');
      apiClient.setConfig({ apiBase: managerURL, managementKey });
      mocks.fetchSnapshot.mockImplementation(actual.configFileApi.fetchSnapshot);
      mocks.saveConfigYaml.mockImplementation(actual.configFileApi.saveConfigYaml);
      const until = async (predicate: () => boolean) => {
        const deadline = Date.now() + 20000;
        while (!predicate()) {
          const errors = renderer!.root.findAllByProps({ className: 'error-box' });
          if (errors.length > 0)
            throw new Error('真实配置页面失败：' + errors.map((node) => node.children.join(' ')).join('; '));
          if (Date.now() > deadline) throw new Error('真实配置事务等待超时');
          await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 20));
          });
        }
      };
      await mountPage();
      await until(() =>
        renderer!.root
          .findAllByType(Button)
          .some((node) => node.props.children === 'config_management.visual.common.edit')
      );
      const commit = async () => {
        await until(
          () => renderer!.root.findAllByProps({ 'data-test': 'confirm-save' }).length === 1
        );
        await click('confirm-save');
        await until(() => renderer!.root.findAllByType('select').length === 0);
      };
      for (const channel of ['kilo', 'codex', 'bps']) {
        await startAdd('m9-' + channel, channel);
        await commit();
      }
      await startAdd('m9-temporary', 'bps');
      await commit();
      const snapshot = await actual.configFileApi.fetchSnapshot({
        scope: { apiBase: managerURL, managementKey },
      });
      const keys = parseYaml(snapshot.yaml).access['api-keys'] as string[];
      await press('config_management.visual.common.edit', keys.indexOf('m9-kilo'));
      fill('m9-kilo-rotated');
      await press('config_management.visual.common.save');
      await commit();
      await press('config_management.visual.common.delete', keys.indexOf('m9-temporary'));
      await act(async () => {
        void getPendingConfirmation().onConfirm();
      });
      await flush();
      await until(
        () => renderer!.root.findAllByProps({ 'data-test': 'confirm-save' }).length === 1
      );
      await click('confirm-save');
      const saved = await actual.configFileApi.fetchSnapshot({
        scope: { apiBase: managerURL, managementKey },
      });
      const access = parseYaml(saved.yaml).access;
      expect(access['api-keys']).toContain(fixture.legacy_key);
      expect(access['api-keys']).not.toContain('m9-temporary');
      expect(access['client-key-scopes']).toEqual(
        expect.arrayContaining([
          { 'api-key': 'm9-kilo-rotated', channel: 'kilo' },
          { 'api-key': 'm9-codex', channel: 'codex' },
          { 'api-key': 'm9-bps', channel: 'bps' },
        ])
      );
      expect(
        access['client-key-scopes'].some((scope: { 'api-key': string }) =>
          [fixture.legacy_key, 'm9-temporary', 'm9-kilo'].includes(scope['api-key'])
        )
      ).toBe(false);
      expect(mocks.saveConfigYaml).toHaveBeenCalledTimes(6);
      const verified = await fetch(controlURL + '/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          keys: { kilo: 'm9-kilo-rotated', codex: 'm9-codex', bps: 'm9-bps' },
          legacy: fixture.legacy_key,
        }),
      });
      expect(verified.status).toBe(200);
    },
    60000
  );
});
