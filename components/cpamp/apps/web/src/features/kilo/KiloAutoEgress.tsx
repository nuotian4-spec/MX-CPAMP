import { useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type {
  KiloAutoAssignmentInput,
  KiloEgress,
  KiloProxyState,
} from '@/services/api/kiloProxies';
import { kiloRetirementReason } from '@/services/api/kiloProxies';
import styles from './KiloAccountsPage.module.scss';

export function KiloAllocationStatus({ egress }: { egress?: KiloEgress }) {
  if (!egress?.allocation_state) return <span>自动分配状态未知</span>;
  const label = {
    manual: '手工出口',
    waiting: '等待空闲固定IP',
    assigned: '已分配独占IP',
    draining: '等待在途排空',
    egress_unavailable: '独占出口当前不可用',
  }[egress.allocation_state];
  return (
    <span>
      {label}
      {(egress.assigned_ips?.length || egress.assigned_ip) && (
        <small>固定IP：{(egress.assigned_ips ?? [egress.assigned_ip]).join('、')}</small>
      )}
      {egress.allocation_reason && (
        <small>
          {egress.allocation_reason === 'storage_failed'
            ? '归属保存待核'
            : egress.allocation_reason === 'specified_conflict'
              ? '指定 IP 已被占用，等待配齐'
              : egress.allocation_reason === 'partial_egress_unavailable'
                ? '部分出口暂不可用'
                : egress.allocation_reason === 'capacity_wait'
                  ? '等待可用 IP 容量'
                  : kiloRetirementReason[egress.allocation_reason]}
        </small>
      )}
    </span>
  );
}
export function KiloAutoAssignmentForm({
  snapshot,
  busy,
  onSave,
  onClose,
}: {
  snapshot: KiloProxyState;
  busy: boolean;
  onSave: (input: KiloAutoAssignmentInput) => Promise<void>;
  onClose: () => void;
}) {
  const [enabled, setEnabled] = useState(snapshot.auto_assignment_enabled ?? false);
  const [limit, setLimit] = useState(
    snapshot.max_unique_accounts ? String(snapshot.max_unique_accounts) : ''
  );
  const [ipCapacity, setIPCapacity] = useState(String(snapshot.ip_concurrency_limit ?? 20));
  const [entryLimit, setEntryLimit] = useState(
    snapshot.ip_entry_limit == null ? '' : String(snapshot.ip_entry_limit)
  );
  const [error, setError] = useState('');
  const submitting = useRef(false);
  const known =
    snapshot.auto_assignment_enabled !== undefined && snapshot.max_unique_accounts !== undefined;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || submitting.current || !known) return;
    if (
      (enabled || limit !== '') &&
      (!/^\d+$/.test(limit) || !Number.isSafeInteger(Number(limit)) || Number(limit) < 1)
    ) {
      setError('每IP累计不同账号上限N应为正整数；0与未填写均不表示无限。');
      return;
    }
    if (
      !/^\d+$/.test(ipCapacity) ||
      Number(ipCapacity) < 1 ||
      Number(ipCapacity) > 100 ||
      (entryLimit !== '' &&
        (!/^\d+$/.test(entryLimit) || Number(entryLimit) < 1 || Number(entryLimit) > 100))
    ) {
      setError('IP 并发与入口限额应为1至100，入口留空跟随 IP 容量。');
      return;
    }
    setError('');
    submitting.current = true;
    try {
      await onSave({
        auto_assignment_enabled: enabled,
        ip_concurrency_limit: Number(ipCapacity),
        ip_entry_limit: entryLimit === '' ? null : Number(entryLimit),
        ...(limit !== '' ? { max_unique_accounts: Number(limit) } : {}),
      });
    } finally {
      submitting.current = false;
    }
  };
  return (
    <form className={styles.form} aria-label="自动独占IP设置" onSubmit={submit}>
      <ToggleSwitch
        label="新账号默认自动独占IP"
        checked={enabled}
        onChange={setEnabled}
        disabled={busy || !known}
      />
      <Input
        label="每IP累计不同账号上限N"
        type="number"
        min={1}
        step={1}
        value={limit}
        disabled={busy || !known}
        onChange={(event) => setLimit(event.target.value)}
      />
      <Input
        label="每 IP 并发容量"
        type="number"
        min={1}
        max={100}
        value={ipCapacity}
        disabled={busy || !known}
        onChange={(event) => setIPCapacity(event.target.value)}
      />
      <Input
        label="每入口并发限额"
        type="number"
        min={1}
        max={100}
        value={entryLimit}
        placeholder="跟随 IP 容量"
        disabled={busy || !known}
        onChange={(event) => setEntryLimit(event.target.value)}
      />
      <p className={styles.note}>
        新账号可按 IP
        容量扩展，自动模式按需增加；人工指定集合保存时整组配齐。凭证保存成功与出口配齐分别呈现。
      </p>
      <p className={styles.note}>
        N统计历史不同账号，不是请求并发容量。达到N时当前归属继续使用；归还排空后退役。归还与清空回收站均不清除累计历史。
      </p>
      <p className={styles.note}>
        提高N不自动恢复已退休IP，仍需在回收站显式检测并恢复。关闭自动模式不表示释放已绑定IP或解除额度限制。
      </p>
      {!known && <p role="alert">当前连接尚未返回自动分配配置，状态未知；请刷新后核实。</p>}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="button" variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={busy || !known}>
          保存自动独占设置
        </Button>
      </div>
    </form>
  );
}
export { KiloIPGroups } from './KiloIPRelations';
