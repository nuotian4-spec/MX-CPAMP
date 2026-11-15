import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { KiloAccount } from '@/services/api/kilo';
import { kiloManualApi } from '@/services/api/kiloManual';
import {
  kiloProbesApi,
  kiloProbesError,
  type KiloProbesSnapshot,
  type KiloProbeState,
} from '@/services/api/kiloProbes';
import styles from './KiloAccountsPage.module.scss';

const statusLabel = {
  never: '尚未运行',
  scheduled: '已计划',
  running: '运行中',
  completed: '生成完成',
  incomplete: '回答未完成',
  failed: '生成失败',
  canceled: '已取消',
  skipped: '已跳过',
};
const reasonLabel = {
  incomplete: '回答未完成',
  request_failed: '模型请求失败',
  canceled: '本任务已取消',
  account_disabled: '账号人工停用',
  account_restricted: '账号自动受限',
  account_unavailable: '账号不可执行',
  account_busy: '同账号自动任务忙碌',
};

function ProbeState({ title, state }: { title: string; state: KiloProbeState }) {
  return (
    <section aria-label={title + '运行状态'}>
      <h4>
        {title}：{statusLabel[state.status]}
      </h4>
      <p>
        已保存开关：{state.enabled ? '开启' : '关闭'} · 模型：{state.model || '未选择'} · 周期：
        {state.interval_seconds} 秒
      </p>
      <p>
        下次计划：{state.next_run_at ?? '无'} · 最近开始：{state.last_started_at ?? '无'} ·
        最近结束：{state.last_completed_at ?? '无'}
      </p>
      <p>
        模型发送尝试：{state.model_calls} · 跳过：{state.skipped_count}
      </p>
      {state.error_code && <p>原因：{reasonLabel[state.error_code]}</p>}
    </section>
  );
}

export function KiloProbesPanel({
  account,
  scope,
  connectionRef,
  onClose,
}: {
  account: KiloAccount;
  scope: ApiClientRequestScope;
  connectionRef: string;
  onClose: () => void;
}) {
  const [snapshot, setSnapshot] = useState<KiloProbesSnapshot>();
  const [models, setModels] = useState<string[]>([]);
  const [health, setHealth] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [model, setModel] = useState('');
  const [interval, setInterval] = useState('300');
  const [busy, setBusy] = useState(false);
  const [needsRead, setNeedsRead] = useState(true);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const generation = useRef(0);
  const request = useRef<AbortController | null>(null);
  const live = useRef({ id: account.id, scope, connectionRef });
  live.current = { id: account.id, scope, connectionRef };
  const accept = useCallback(
    (next: KiloProbesSnapshot) => {
      if (
        next.account_id !== account.id ||
        next.connection_ref !== connectionRef ||
        !Number.isSafeInteger(next.revision) ||
        next.revision < 0
      )
        throw { status: 409 };
      setSnapshot(next);
      setHealth(next.health.enabled);
      setEnabled(next.model_probe.enabled);
      setModel(next.model_probe.model);
      setInterval(String(next.model_probe.interval_seconds));
      setNeedsRead(false);
    },
    [account.id, connectionRef]
  );
  const load = useCallback(async () => {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    const version = generation.current;
    const current = () =>
      version === generation.current &&
      !controller.signal.aborted &&
      live.current.id === account.id &&
      live.current.scope === scope &&
      live.current.connectionRef === connectionRef;
    setBusy(true);
    setError('');
    setNote('');
    try {
      const [next, catalog] = await Promise.all([
        kiloProbesApi.get(account.id, scope, controller.signal),
        kiloManualApi.models(scope, controller.signal),
      ]);
      if (!current()) return;
      if (catalog.connection_ref !== connectionRef || !catalog.models.length) throw { status: 409 };
      accept(next);
      setModels(catalog.models.map((item) => item.id));
    } catch (cause) {
      if (current()) {
        setNeedsRead(true);
        setError(kiloProbesError(cause));
      }
    } finally {
      if (version === generation.current && request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  }, [account.id, scope, connectionRef, accept]);
  useEffect(() => {
    generation.current++;
    setSnapshot(undefined);
    setModels([]);
    setHealth(false);
    setEnabled(false);
    setModel('');
    setInterval('300');
    setNeedsRead(true);
    void load();
    const invalidate = () => {
      generation.current++;
      request.current?.abort();
      request.current = null;
    };
    const unauthorized = () => {
      invalidate();
      setSnapshot(undefined);
      setModels([]);
      setHealth(false);
      setEnabled(false);
      setModel('');
      setNeedsRead(true);
      setBusy(false);
      setNote('');
      setError(kiloProbesError({ status: 401 }));
    };
    globalThis.window?.addEventListener('unauthorized', unauthorized);
    return () => {
      invalidate();
      globalThis.window?.removeEventListener('unauthorized', unauthorized);
    };
  }, [load]);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (request.current || !snapshot || needsRead) return;
    if (
      !/^\d+$/.test(interval) ||
      Number(interval) < 60 ||
      Number(interval) > 86400 ||
      (model !== '' && model !== 'kilo-auto/free' && !models.includes(model)) ||
      (enabled && !model)
    ) {
      setError('启用模型周期探测需明确选择模型；周期应为60–86400整数秒。');
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    const version = generation.current;
    const current = () =>
      version === generation.current &&
      !controller.signal.aborted &&
      live.current.id === account.id &&
      live.current.scope === scope &&
      live.current.connectionRef === connectionRef;
    setBusy(true);
    setError('');
    setNote('');
    try {
      const next = await kiloProbesApi.save(
        account.id,
        {
          expected_revision: snapshot.revision,
          health: { enabled: health },
          model_probe: { enabled, model, interval_seconds: Number(interval) },
        },
        connectionRef,
        scope,
        controller.signal
      );
      if (!current()) return;
      accept(next);
      setNote('自动生成配置已保存；状态以服务器投影为准，未主动触发立即生成。');
    } catch (cause) {
      if (current()) {
        setNeedsRead(true);
        setError(kiloProbesError(cause));
      }
    } finally {
      if (version === generation.current && request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  };
  const disabled = busy || !snapshot || needsRead;
  return (
    <form className={styles.form} onSubmit={submit}>
      <p>
        目标账号：<strong>{account.name || account.id}</strong> · {account.id}
      </p>

      <p className={styles.note}>
        默认关闭；开启会发送模型请求，可能计费。关闭页面不会停止已保存的任务。
      </p>
      <fieldset disabled={disabled}>
        <legend>账号健康生成</legend>
        <ToggleSwitch
          label="开启账号健康生成"
          checked={health}
          onChange={setHealth}
          disabled={disabled}
        />
        <p>固定模型 kilo-auto/free · 固定提示词 hi · 周期300秒；不是代理IP健康检查。</p>
      </fieldset>
      <fieldset disabled={disabled}>
        <legend>模型周期探测</legend>
        <ToggleSwitch
          label="开启模型周期探测"
          checked={enabled}
          onChange={setEnabled}
          disabled={disabled}
        />
        <label>
          探测模型
          <Select
            ariaLabel="探测模型"
            value={model}
            onChange={setModel}
            disabled={disabled}
            options={[
              { value: '', label: '请选择模型' },
              ...models.map((id) => ({ value: id, label: id })),
              { value: 'kilo-auto/free', label: 'kilo-auto/free（管理专用）' },
            ]}
          />
        </label>
        <Input
          label="周期（秒）"
          aria-label="探测周期秒数"
          type="number"
          min={60}
          max={86400}
          step={1}
          value={interval}
          onChange={(event) => setInterval(event.target.value)}
          disabled={disabled}
        />
        <p>固定提示词 hi；首次启用或修改模型/周期后，从保存成功后的完整周期开始，不立即发送。</p>
      </fieldset>
      <details>
        <summary>运行规则</summary>{' '}
        <p className={styles.note}>
          后台由服务器驱动，关闭页面不会关闭任务。关闭或更改任务需保存配置；关闭本页仅中止本页请求，服务器保存结果待核。同账号两类同时到期时交替运行，忙碌跳过不是失败或恢复。停用/受限账号不发送，成功不暗启用或解除限制。
        </p>
      </details>
      {snapshot && (
        <section aria-label="自动生成已读取状态">
          <h3>自动任务状态</h3>
          <details>
            <summary>诊断与计数</summary>
            <p>
              当前宿主计数起点：{snapshot.runtime_started_at} · 手动模型发送尝试：
              {snapshot.manual_model_calls}
            </p>
            <p>
              计数为实际模型Transport发送尝试，不等同成功或计费次数；两类按类别累计，切换模型不清零，不是当前模型专属次数。重启归零，不是历史总量；额度查询和代理检查另计。
            </p>
          </details>
          <ProbeState title="账号健康生成" state={snapshot.health} />
          <ProbeState title="模型周期探测" state={snapshot.model_probe} />
        </section>
      )}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {note && <p role="status">{note}</p>}
      {busy && <p role="status">正在读取或保存配置，不在本页触发模型生成。</p>}
      <div className={styles.actions}>
        <Button type="submit" disabled={disabled}>
          保存自动生成配置
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={busy}
          onClick={() => {
            void load();
          }}
        >
          重新读取状态
        </Button>
        <Button type="button" variant="secondary" onClick={onClose}>
          关闭
        </Button>
      </div>
      <p className={styles.note}>
        重新读取会替换未保存草稿；读取本身不生成模型内容，也不自动开启任务。
      </p>
    </form>
  );
}
