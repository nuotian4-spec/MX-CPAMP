import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import type { UpdateOperation } from '@/types/pluginImport';
import styles from './SystemUpdateSection.module.scss';

export interface CoreUpdateDiscovery {
  site_id: string;
  registered: boolean;
  current_version: string;
  official_version?: string;
  site_version?: string;
  releases: { id: string; version: string; notes: string }[];
  active_operation?: UpdateOperation;
  blockers: string[];
}

export interface CoreUpdatePreview {
  id: string;
  site_id: string;
  version: string;
  current_version: string;
  maintenance: string[];
  notes: string;
  installable: boolean;
  blockers: string[];
}

export interface SystemUpdateSectionProps {
  connectionScope: string;
  disabled?: boolean;
  api: {
    discover(): Promise<CoreUpdateDiscovery>;
    preview(releaseId: string): Promise<CoreUpdatePreview>;
    install(candidateId: string): Promise<UpdateOperation>;
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
const STATUS_LABELS: Record<UpdateOperation['status'], string> = {
  queued: '等待核心更新',
  installing: '正在更新核心',
  reloading: '正在重新加载核心',
  completed: '核心更新完成',
  completed_disabled: '更新结束，组件保持停用',
  failed: '核心更新失败',
  license_unverified: '许可尚未验证',
  license_error: '许可验证失败',
  awaiting_restart: '等待重启，更新尚未完成',
  recovery_required: '需要恢复，更新尚未完成',
};
const errorMessage = (cause: unknown) =>
  cause instanceof Error ? cause.message : '请求失败，请稍后重试';

export function SystemUpdateSection(props: SystemUpdateSectionProps) {
  return (
    <UpdateSession key={JSON.stringify([props.connectionScope, !!props.disabled])} {...props} />
  );
}

function UpdateSession({ api, disabled = false }: SystemUpdateSectionProps) {
  const requestApi = useRef(api).current;
  const [discovery, setDiscovery] = useState<CoreUpdateDiscovery | null>(null);
  const [selectedRelease, setSelectedRelease] = useState('');
  const [candidate, setCandidate] = useState<CoreUpdatePreview | null>(null);
  const [impactConfirmed, setImpactConfirmed] = useState(false);
  const [busy, setBusy] = useState<'discover' | 'preview' | 'install' | null>(null);
  const [maintenance, setMaintenance] = useState(false);
  const [operation, setOperation] = useState<UpdateOperation | null>(null);
  const [querying, setQuerying] = useState(false);
  const [confirmedTerminal, setConfirmedTerminal] = useState(false);
  const [error, setError] = useState('');
  const alive = useRef(true);
  const epoch = useRef(0);
  const actionPending = useRef(false);
  const maintenanceStarted = useRef(false);
  const operationId = useRef('');
  const terminalConfirmed = useRef(false);
  const queryPending = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isCurrent = useCallback(
    (requestEpoch: number) => alive.current && epoch.current === requestEpoch,
    []
  );

  const queryOperation = useCallback(
    async function query(id: string, requestEpoch: number) {
      if (!isCurrent(requestEpoch) || queryPending.current) return;
      queryPending.current = true;
      setQuerying(true);
      setError('');
      let result: UpdateOperation;
      try {
        result = await requestApi.getOperation(id);
        if (!isCurrent(requestEpoch)) return;
        if (result.id !== id) throw new Error('返回的更新任务与当前任务不一致');
      } catch (cause) {
        if (isCurrent(requestEpoch)) {
          queryPending.current = false;
          setQuerying(false);
          setError(`状态查询失败：${errorMessage(cause)}。重试只查询原任务，不重复安装。`);
        }
        return;
      }
      queryPending.current = false;
      setOperation(result);
      const terminal = TERMINAL_STATUSES.has(result.status);
      terminalConfirmed.current = terminal;
      setConfirmedTerminal(terminal);
      setQuerying(!terminal);
      if (!terminal) {
        timer.current = setTimeout(() => void query(id, requestEpoch), 1500);
      }
    },
    [isCurrent, requestApi]
  );

  const discover = useCallback(async () => {
    if (
      !alive.current ||
      disabled ||
      actionPending.current ||
      (operationId.current && !terminalConfirmed.current)
    )
      return;
    actionPending.current = true;
    const requestEpoch = ++epoch.current;
    if (timer.current !== null) clearTimeout(timer.current);
    queryPending.current = false;
    maintenanceStarted.current = false;
    operationId.current = '';
    terminalConfirmed.current = false;
    setBusy('discover');
    setDiscovery(null);
    setCandidate(null);
    setSelectedRelease('');
    setImpactConfirmed(false);
    setOperation(null);
    setMaintenance(false);
    setConfirmedTerminal(false);
    setQuerying(false);
    setError('');
    try {
      const result = await requestApi.discover();
      if (!isCurrent(requestEpoch)) return;
      setDiscovery(result);
      if (result.active_operation) {
        if (!result.active_operation.id) throw new Error('后台更新任务未返回任务编号');
        operationId.current = result.active_operation.id;
        maintenanceStarted.current = true;
        setMaintenance(true);
        setOperation(result.active_operation);
        void queryOperation(result.active_operation.id, requestEpoch);
      }
    } catch (cause) {
      if (isCurrent(requestEpoch)) setError(`版本与任务读取失败：${errorMessage(cause)}`);
    } finally {
      if (isCurrent(requestEpoch)) {
        actionPending.current = false;
        setBusy(null);
      }
    }
  }, [disabled, isCurrent, queryOperation, requestApi]);

  useEffect(() => {
    alive.current = true;
    if (!disabled) void discover();
    return () => {
      alive.current = false;
      epoch.current += 1;
      actionPending.current = false;
      queryPending.current = false;
      if (timer.current !== null) clearTimeout(timer.current);
    };
  }, [disabled, discover]);

  const selected = discovery?.releases.find((release) => release.id === selectedRelease);
  const canPreview =
    !disabled &&
    !!discovery?.registered &&
    discovery.blockers.length === 0 &&
    !!selected &&
    !maintenance;
  const canInstall =
    canPreview && !!candidate?.installable && candidate.blockers.length === 0 && impactConfirmed;

  const preview = async () => {
    if (!alive.current || actionPending.current || !canPreview || !discovery) return;
    actionPending.current = true;
    const requestEpoch = ++epoch.current;
    setBusy('preview');
    setCandidate(null);
    setImpactConfirmed(false);
    setError('');
    try {
      const result = await requestApi.preview(selectedRelease);
      if (!isCurrent(requestEpoch)) return;
      if (!result.id || result.site_id !== discovery.site_id) {
        throw new Error('预览候选与当前站点不一致');
      }
      setCandidate(result);
    } catch (cause) {
      if (isCurrent(requestEpoch)) setError(`生成更新预览失败：${errorMessage(cause)}`);
    } finally {
      if (isCurrent(requestEpoch)) {
        actionPending.current = false;
        setBusy(null);
      }
    }
  };

  const install = async () => {
    if (
      !alive.current ||
      actionPending.current ||
      maintenanceStarted.current ||
      !canInstall ||
      !candidate
    )
      return;
    actionPending.current = true;
    maintenanceStarted.current = true;
    const requestEpoch = ++epoch.current;
    setBusy('install');
    setMaintenance(true);
    setQuerying(true);
    setError('');
    try {
      const accepted = await requestApi.install(candidate.id);
      if (!isCurrent(requestEpoch)) return;
      if (!accepted.id) throw new Error('更新请求未返回任务编号');
      operationId.current = accepted.id;
      setOperation(accepted);
      void queryOperation(accepted.id, requestEpoch);
    } catch (cause) {
      if (isCurrent(requestEpoch)) {
        setQuerying(false);
        setError(`更新请求未确认：${errorMessage(cause)}。请刷新版本与任务，确认后台状态。`);
      }
    } finally {
      if (isCurrent(requestEpoch)) {
        actionPending.current = false;
        setBusy(null);
      }
    }
  };

  const successful = confirmedTerminal && operation?.status === 'completed';
  const failed = confirmedTerminal && ['failed', 'license_error'].includes(operation?.status ?? '');
  const attention =
    operation &&
    ['completed_disabled', 'awaiting_restart', 'recovery_required', 'license_unverified'].includes(
      operation.status
    );
  const state = successful ? 'success' : failed ? 'failed' : attention ? 'attention' : 'pending';

  return (
    <Card
      title="核心更新"
      extra={
        <Button
          type="button"
          variant="secondary"
          size="sm"
          loading={busy === 'discover'}
          disabled={disabled || busy !== null || (!!operation && !confirmedTerminal)}
          onClick={() => void discover()}
        >
          刷新版本与任务
        </Button>
      }
    >
      <div className={styles.content}>
        {disabled ? (
          <div className={styles.warning} role="status">
            连接不可用，核心更新查询已暂停；重连后重新读取后台任务。
          </div>
        ) : (
          <>
            {busy === 'discover' && <div className={styles.hint}>正在读取版本与后台任务…</div>}
            {discovery && (
              <>
                <dl className={styles.details}>
                  <dt>站点</dt>
                  <dd>{discovery.site_id}</dd>
                  <dt>注册状态</dt>
                  <dd>{discovery.registered ? '已注册' : '未注册'}</dd>
                  <dt>当前版本</dt>
                  <dd>{discovery.current_version || '待确认'}</dd>
                  <dt>官方发现版本</dt>
                  <dd>{discovery.official_version || '未发现'}</dd>
                  <dt>本站发布版本</dt>
                  <dd>{discovery.site_version || '未发现'}</dd>
                </dl>
                <p className={styles.hint}>官方版本仅用于发现与对比，安装仅接受本站发布版本。</p>
                {!discovery.registered && (
                  <div className={styles.warning}>当前站点尚未注册，更新入口已停用。</div>
                )}
                {discovery.blockers.length > 0 && (
                  <ul className={styles.blockers}>
                    {discovery.blockers.map((reason, index) => (
                      <li key={`${index}:${reason}`}>{reason}</li>
                    ))}
                  </ul>
                )}
                {!maintenance && (
                  <>
                    <div className={styles.selection}>
                      <label className={styles.releaseLabel}>
                        <span>本站发布版本</span>
                        <select
                          aria-label="本站发布版本"
                          value={selectedRelease}
                          disabled={
                            busy !== null || !discovery.registered || discovery.blockers.length > 0
                          }
                          onChange={(event) => {
                            setSelectedRelease(event.target.value);
                            setCandidate(null);
                            setImpactConfirmed(false);
                            setError('');
                          }}
                        >
                          <option value="">请选择本站发布版本</option>
                          {discovery.releases.map((release) => (
                            <option key={release.id} value={release.id}>
                              {release.version}
                            </option>
                          ))}
                        </select>
                      </label>
                      <Button
                        type="button"
                        variant="secondary"
                        loading={busy === 'preview'}
                        disabled={!canPreview || busy !== null}
                        onClick={() => void preview()}
                      >
                        生成更新预览
                      </Button>
                    </div>
                    {discovery.releases.length === 0 && (
                      <div className={styles.hint}>本站暂无可选发布版本。</div>
                    )}
                    {!candidate && selected?.notes && (
                      <div className={styles.notes}>{selected.notes}</div>
                    )}
                  </>
                )}
              </>
            )}
            {candidate && (
              <section className={styles.preview} aria-label="固定更新候选">
                <strong>固定候选：{candidate.version}</strong>
                <div>候选编号：{candidate.id}</div>
                <div>
                  站点：{candidate.site_id}；当前版本：{candidate.current_version}
                </div>
                {candidate.notes && <div className={styles.notes}>{candidate.notes}</div>}
                <strong>维护与备份影响</strong>
                <div>请确认维护窗口与备份安排；更新期间服务可能中断。</div>
                {candidate.maintenance.length > 0 && (
                  <ul className={styles.steps}>
                    {candidate.maintenance.map((step, index) => (
                      <li key={`${index}:${step}`}>{step}</li>
                    ))}
                  </ul>
                )}
                {candidate.blockers.length > 0 && (
                  <ul className={styles.blockers}>
                    {candidate.blockers.map((reason, index) => (
                      <li key={`${index}:${reason}`}>{reason}</li>
                    ))}
                  </ul>
                )}
                {!candidate.installable && (
                  <div className={styles.warning}>此固定候选当前不可安装。</div>
                )}
                {!maintenance && (
                  <>
                    <label className={styles.confirmation}>
                      <input
                        type="checkbox"
                        aria-label="确认维护备份影响"
                        checked={impactConfirmed}
                        disabled={
                          !candidate.installable || candidate.blockers.length > 0 || busy !== null
                        }
                        onChange={(event) => setImpactConfirmed(event.target.checked)}
                      />
                      <span>我已确认本次固定候选的维护、备份及服务中断影响</span>
                    </label>
                    <Button
                      type="button"
                      disabled={!canInstall || busy !== null}
                      onClick={() => void install()}
                    >
                      确认更新此候选
                    </Button>
                  </>
                )}
              </section>
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
                data-core-update-state={state}
              >
                <strong>
                  {operation
                    ? !confirmedTerminal && TERMINAL_STATUSES.has(operation.status)
                      ? '正在核验核心更新结果'
                      : STATUS_LABELS[operation.status]
                    : '正在提交核心更新任务'}
                </strong>
                {operation && (
                  <>
                    <div>任务编号：{operation.id}</div>
                    <div>
                      目标版本：{operation.target_version}；活动版本：
                      {operation.active_version || '待确认'}
                    </div>
                    {operation.stage && <div>当前阶段：{operation.stage}</div>}
                    <div>注册状态：{operation.registered ? '已注册' : '未注册'}</div>
                    {operation.message && <div>{operation.message}</div>}
                  </>
                )}
                {!confirmedTerminal && (
                  <div className={styles.hint}>
                    断线不代表更新成功；恢复连接后将重新查询后台任务。
                  </div>
                )}
                {operation && !querying && !confirmedTerminal && error && (
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => void queryOperation(operation.id, epoch.current)}
                  >
                    重试状态查询
                  </Button>
                )}
              </section>
            )}
          </>
        )}
        {error && (
          <div className={styles.error} role="alert">
            {error}
          </div>
        )}
      </div>
    </Card>
  );
}
