import { useEffect, useRef, useState, type ChangeEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import type { PluginImportPreview, UpdateOperation } from '@/types/pluginImport';
import styles from './PluginZipImportModal.module.scss';

export interface PluginZipImportModalProps {
  isOpen: boolean;
  connectionScope: string;
  onClose: () => void;
  onInstalled: () => void;
  api: {
    upload(file: File): Promise<PluginImportPreview>;
    cancel(id: string): Promise<void>;
    install(
      id: string,
      confirmation: { confirm_unverified: boolean; confirm_license_network: boolean }
    ): Promise<UpdateOperation>;
    getOperation(id: string): Promise<UpdateOperation>;
  };
}

const TERMINAL_STATUSES = new Set<UpdateOperation['status']>([
  'completed',
  'completed_disabled',
  'failed',
  'license_unverified',
  'license_error',
]);
const SUCCESS_STATUSES = new Set<UpdateOperation['status']>(['completed', 'completed_disabled']);
const hasUnverifiedLicense = (operation: UpdateOperation, preview: PluginImportPreview | null) =>
  ['unknown', 'unverified', 'license_unverified', 'required'].includes(operation.license_status) ||
  (preview?.license_status === 'unknown' && operation.license_status !== 'verified');
const STATUS_LABELS: Record<UpdateOperation['status'], string> = {
  queued: '等待安装',
  installing: '正在安装',
  reloading: '正在重新加载插件',
  completed: '安装完成',
  completed_disabled: '安装完成，插件保持停用',
  failed: '安装失败',
  license_unverified: '许可尚未验证',
  license_error: '许可验证失败',
  awaiting_restart: '等待重启，安装尚未完成',
  recovery_required: '需要恢复，安装尚未完成',
};
const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : '请求失败，请稍后重试';

export function PluginZipImportModal(props: PluginZipImportModalProps) {
  return props.isOpen ? <ImportSession key={props.connectionScope} {...props} /> : null;
}

function ImportSession({ onClose, onInstalled, api }: PluginZipImportModalProps) {
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<PluginImportPreview | null>(null);
  const [operation, setOperation] = useState<UpdateOperation | null>(null);
  const [uploading, setUploading] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [maintenance, setMaintenance] = useState(false);
  const [querying, setQuerying] = useState(false);
  const [confirmedTerminal, setConfirmedTerminal] = useState(false);
  const [confirmUnverified, setConfirmUnverified] = useState(false);
  const [confirmLicenseNetwork, setConfirmLicenseNetwork] = useState(false);
  const [error, setError] = useState('');
  const alive = useRef(true);
  const epoch = useRef(0);
  const pendingAction = useRef(false);
  const maintenanceStarted = useRef(false);
  const notified = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      epoch.current += 1;
      if (timer.current !== null) clearTimeout(timer.current);
    };
  }, []);

  const isCurrent = (requestEpoch: number) => alive.current && epoch.current === requestEpoch;
  const dismiss = () => {
    alive.current = false;
    epoch.current += 1;
    if (timer.current !== null) clearTimeout(timer.current);
    onClose();
  };

  const close = async () => {
    if (!alive.current || cancelling) return;
    if (maintenanceStarted.current || !preview) {
      dismiss();
      return;
    }
    if (pendingAction.current) return;
    pendingAction.current = true;
    const requestEpoch = ++epoch.current;
    setCancelling(true);
    setError('');
    try {
      await api.cancel(preview.id);
      if (isCurrent(requestEpoch)) dismiss();
    } catch (cause) {
      if (isCurrent(requestEpoch)) setError(`取消导入失败：${errorMessage(cause)}`);
    } finally {
      if (isCurrent(requestEpoch)) {
        pendingAction.current = false;
        setCancelling(false);
      }
    }
  };

  const selectFile = (event: ChangeEvent<HTMLInputElement>) => {
    const next = event.target.files?.[0] ?? null;
    setFile(next);
    setError(next && !next.name.toLowerCase().endsWith('.zip') ? '请选择 ZIP 格式的插件包' : '');
  };

  const upload = async () => {
    if (
      !alive.current ||
      pendingAction.current ||
      !file ||
      !file.name.toLowerCase().endsWith('.zip')
    )
      return;
    pendingAction.current = true;
    const requestEpoch = ++epoch.current;
    setUploading(true);
    setError('');
    try {
      const result = await api.upload(file);
      if (isCurrent(requestEpoch)) {
        setConfirmUnverified(false);
        setConfirmLicenseNetwork(false);
        setPreview(result);
      }
    } catch (cause) {
      if (isCurrent(requestEpoch)) setError(`上传失败：${errorMessage(cause)}`);
    } finally {
      if (isCurrent(requestEpoch)) {
        pendingAction.current = false;
        setUploading(false);
      }
    }
  };

  const queryOperation = async (id: string, requestEpoch: number) => {
    if (!isCurrent(requestEpoch)) return;
    setQuerying(true);
    setError('');
    let result: UpdateOperation;
    try {
      result = await api.getOperation(id);
      if (!isCurrent(requestEpoch)) return;
      if (result.id !== id) throw new Error('返回的安装任务与当前任务不一致');
    } catch (cause) {
      if (isCurrent(requestEpoch)) {
        setQuerying(false);
        setError(`状态查询失败：${errorMessage(cause)}。可重试查询，后台任务不会重复提交。`);
      }
      return;
    }
    setOperation(result);
    const terminal = TERMINAL_STATUSES.has(result.status);
    setConfirmedTerminal(terminal);
    setQuerying(!terminal);
    if (terminal) {
      if (
        SUCCESS_STATUSES.has(result.status) &&
        !hasUnverifiedLicense(result, preview) &&
        !notified.current
      ) {
        notified.current = true;
        onInstalled();
      }
      return;
    }
    timer.current = setTimeout(() => {
      void queryOperation(id, requestEpoch);
    }, 1500);
  };

  const candidateAllowed =
    preview !== null && preview.compatibility !== 'incompatible' && preview.installable;
  const licenseNeedsConfirmation =
    !!preview && (preview.license_required || preview.license_status === 'unknown');
  const installAllowed =
    candidateAllowed &&
    (preview.compatibility !== 'unverified' || confirmUnverified) &&
    (!licenseNeedsConfirmation || confirmLicenseNetwork);

  const install = async () => {
    if (
      !alive.current ||
      pendingAction.current ||
      maintenanceStarted.current ||
      !preview ||
      !installAllowed
    )
      return;
    pendingAction.current = true;
    maintenanceStarted.current = true;
    const requestEpoch = ++epoch.current;
    setMaintenance(true);
    setQuerying(true);
    setError('');
    try {
      const accepted = await api.install(preview.id, {
        confirm_unverified: preview.compatibility === 'unverified' && confirmUnverified,
        confirm_license_network: licenseNeedsConfirmation && confirmLicenseNetwork,
      });
      if (!isCurrent(requestEpoch)) return;
      if (!accepted.id) throw new Error('安装请求未返回任务编号');
      setOperation(accepted);
      void queryOperation(accepted.id, requestEpoch);
    } catch (cause) {
      if (isCurrent(requestEpoch)) {
        setQuerying(false);
        setError(
          `安装请求未确认：${errorMessage(cause)}。请在更新中心确认任务状态，避免重复提交。`
        );
      }
    }
  };

  const licenseUnverified = !!operation && hasUnverifiedLicense(operation, preview);
  const successful =
    confirmedTerminal && operation && SUCCESS_STATUSES.has(operation.status) && !licenseUnverified;
  const attention =
    operation &&
    (['awaiting_restart', 'recovery_required', 'license_unverified'].includes(operation.status) ||
      licenseUnverified);
  const failed =
    confirmedTerminal && operation && ['failed', 'license_error'].includes(operation.status);

  return (
    <Modal
      open
      title="导入插件 ZIP"
      onClose={() => {
        void close();
      }}
      closeDisabled={cancelling}
      width={620}
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              void close();
            }}
            loading={cancelling}
          >
            {maintenance ? '关闭' : '取消导入'}
          </Button>
          {!maintenance && !preview && (
            <Button
              type="button"
              onClick={() => {
                void upload();
              }}
              loading={uploading}
              disabled={!file || !file.name.toLowerCase().endsWith('.zip')}
            >
              上传并预览
            </Button>
          )}
          {!maintenance && preview && (
            <Button
              type="button"
              onClick={() => {
                void install();
              }}
              disabled={!installAllowed || cancelling}
            >
              确认安装
            </Button>
          )}
          {maintenance && operation && !querying && !confirmedTerminal && error && (
            <Button
              type="button"
              onClick={() => {
                void queryOperation(operation.id, epoch.current);
              }}
            >
              重试查询
            </Button>
          )}
        </>
      }
    >
      <div className={styles.content}>
        {!preview && !maintenance && (
          <label className={styles.filePicker}>
            <span>选择 ZIP 文件</span>
            <input
              type="file"
              accept=".zip,application/zip,application/x-zip-compressed"
              aria-label="选择 ZIP 文件"
              disabled={uploading}
              onChange={selectFile}
            />
            <span className={styles.hint}>上传只生成导入预览；确认安装前不会替换现有插件。</span>
          </label>
        )}
        {preview && (
          <>
            <dl className={styles.details}>
              <dt>插件标识</dt>
              <dd>{preview.plugin_id}</dd>
              <dt>导入版本</dt>
              <dd>{preview.version}</dd>
              <dt>当前版本</dt>
              <dd>{preview.current_version || '尚未安装'}</dd>
              <dt>目标平台</dt>
              <dd>{preview.platform}</dd>
              <dt>插件库</dt>
              <dd>{preview.library_path}</dd>
              <dt>原启用状态</dt>
              <dd>{preview.currently_enabled ? '已启用' : '已停用／尚未启用，安装不会自动启用'}</dd>
              <dt>兼容性</dt>
              <dd>
                {preview.compatibility === 'verified'
                  ? '已验证'
                  : preview.compatibility === 'incompatible'
                    ? '不兼容'
                    : '尚未验证'}
              </dd>
            </dl>
            <div className={styles.notice}>
              {preview.license_status === 'unknown'
                ? '此插件的许可需求尚未知。安装、启动或重新加载可能连接插件许可服务；显式确认不会把许可状态变为已验证或无需许可。'
                : preview.license_required
                  ? '此插件需要许可验证。安装、启动或重新加载可能连接插件许可服务；联网或许可失败不代表安装成功。'
                  : '导入预览未声明必需的许可验证。'}
            </div>
            {!maintenance && !candidateAllowed && (
              <div className={styles.warning} role="status">
                当前包不兼容或存在安装阻断项，请处理后重新导入。
              </div>
            )}
            {!maintenance &&
              (preview.compatibility === 'unverified' || licenseNeedsConfirmation) && (
                <div className={styles.confirmations} role="group" aria-label="安装确认">
                  {preview.compatibility === 'unverified' && (
                    <label className={styles.confirmation}>
                      <input
                        type="checkbox"
                        aria-label="我确认本固定候选兼容性未验证"
                        checked={confirmUnverified}
                        disabled={!candidateAllowed || cancelling}
                        onChange={(event) => setConfirmUnverified(event.target.checked)}
                      />
                      <span>
                        我确认本固定候选兼容性未验证
                        <span className={styles.confirmationHint}>
                          此确认只用于本次安装，兼容性仍标记为尚未验证。
                        </span>
                      </span>
                    </label>
                  )}
                  {licenseNeedsConfirmation && (
                    <label className={styles.confirmation}>
                      <input
                        type="checkbox"
                        aria-label="我确认启动或重新加载可能联网验证许可"
                        checked={confirmLicenseNetwork}
                        disabled={!candidateAllowed || cancelling}
                        onChange={(event) => setConfirmLicenseNetwork(event.target.checked)}
                      />
                      <span>我确认启动或重新加载可能联网验证许可</span>
                    </label>
                  )}
                </div>
              )}
            {preview.blockers.length > 0 && (
              <ul className={styles.blockers}>
                {preview.blockers.map((reason, index) => (
                  <li key={`${index}:${reason}`}>{reason}</li>
                ))}
              </ul>
            )}
          </>
        )}
        {maintenance && (
          <section
            className={
              successful
                ? styles.success
                : failed
                  ? styles.error
                  : attention
                    ? styles.warning
                    : styles.progress
            }
            aria-live="polite"
            aria-busy={querying}
            data-import-state={
              successful ? 'success' : failed ? 'failed' : attention ? 'attention' : 'pending'
            }
          >
            <strong>
              {operation
                ? !confirmedTerminal && TERMINAL_STATUSES.has(operation.status)
                  ? '正在核验安装结果'
                  : confirmedTerminal && licenseUnverified && !failed
                    ? '许可尚未验证'
                    : STATUS_LABELS[operation.status]
                : '正在提交安装任务'}
            </strong>
            {operation && (
              <>
                <div>任务编号：{operation.id}</div>
                {operation.stage && <div>当前阶段：{operation.stage}</div>}
                <div>
                  活动版本：{operation.active_version || '待确认'}；注册状态：
                  {operation.registered ? '已注册' : '未注册'}
                </div>
                {operation.license_status && (
                  <div>许可状态：{licenseUnverified ? '尚未验证' : operation.license_status}</div>
                )}
                {operation.message && <div>{operation.message}</div>}
              </>
            )}
            {!confirmedTerminal && (
              <div className={styles.hint}>
                关闭窗口不会取消后台安装；等待重启或需要恢复时，任务仍未完成。
              </div>
            )}
          </section>
        )}
        {error && (
          <div className={styles.error} role="alert">
            {error}
          </div>
        )}
      </div>
    </Modal>
  );
}
