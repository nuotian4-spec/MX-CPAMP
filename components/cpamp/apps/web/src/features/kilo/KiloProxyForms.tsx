import { KiloEgressFields } from './KiloEgressFields';
import { validateKiloEgressInput } from './kiloEgressInput';
import { useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { KiloAccount } from '@/services/api/kilo';
import type {
  KiloEgressInput,
  KiloProxy,
  KiloProxyInput,
  KiloProxyState,
} from '@/services/api/kiloProxies';
import styles from './KiloAccountsPage.module.scss';

export function KiloProxyForm({
  proxy,
  accounts,
  hasManualReferences = false,
  busy,
  onSave,
  onClose,
}: {
  proxy?: KiloProxy;
  accounts: KiloAccount[];
  hasManualReferences?: boolean;
  busy: boolean;
  onSave: (input: KiloProxyInput) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(proxy?.name ?? '');
  const [url, setUrl] = useState('');
  const [order, setOrder] = useState(String(proxy?.order ?? 0));
  const [enabled, setEnabled] = useState(proxy?.enabled ?? true);
  const [accountID, setAccountID] = useState(proxy?.account_id ?? '');
  const [error, setError] = useState('');
  const [confirmFixedIP, setConfirmFixedIP] = useState(false);
  const submitting = useRef(false);
  const fixedConflict = !!proxy?.account_id || hasManualReferences || !!accountID;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || submitting.current) return;
    const secret = url;
    setUrl('');
    setError('');
    if (!proxy && !secret.trim()) {
      setError('增加代理时请填写完整URL。');
      return;
    }
    if (proxy && (!/^\d+$/.test(order) || !Number.isSafeInteger(Number(order)))) {
      setError('顺序应为非负整数。');
      return;
    }
    if (confirmFixedIP && (fixedConflict || secret.trim())) {
      setError(
        '请先保存解除手工归属与出口引用，再重新打开编辑确认固定IP；替换URL须先完成健康重验。'
      );
      return;
    }
    const input: KiloProxyInput = {
      name,
      enabled,
      account_id: accountID,
      ...(proxy ? { order: Number(order) } : {}),
      ...(secret.trim() ? { url: secret.trim() } : {}),
      ...(proxy && confirmFixedIP && !secret.trim() && proxy.observed_exit_ip
        ? { fixed_exit_ip: proxy.observed_exit_ip }
        : {}),
    };
    submitting.current = true;
    try {
      await onSave(input);
    } finally {
      submitting.current = false;
    }
  };
  return (
    <form className={styles.form} autoComplete="off" onSubmit={submit}>
      <Input
        label="代理名称"
        value={name}
        onChange={(e) => setName(e.target.value)}
        disabled={busy}
      />
      <Input
        label={proxy ? '替换代理URL（留空保留）' : '代理URL'}
        type="password"
        autoComplete="new-password"
        value={url}
        onChange={(e) => {
          setUrl(e.target.value);
          setConfirmFixedIP(false);
        }}
        disabled={busy}
        hint="URL仅写入服务端；提交或关闭后清空，原认证信息不回显。"
      />
      {proxy && (
        <Input
          label="代理顺序"
          type="number"
          min={0}
          step={1}
          value={order}
          onChange={(e) => setOrder(e.target.value)}
          disabled={busy}
        />
      )}
      <div>
        <p className={styles.note}>专属归属</p>
        <Select
          ariaLabel="专属归属"
          value={accountID}
          onChange={(value) => {
            setAccountID(value);
            setConfirmFixedIP(false);
          }}
          disabled={busy || !!proxy?.fixed_exit_ip}
          options={[
            { value: '', label: '共享代理' },
            ...accounts.map((a) => ({ value: a.id, label: a.name || a.id })),
          ]}
        />
        {proxy?.fixed_exit_ip && (
          <p className={styles.note}>该固定IP属于自动独占池，手工归属保持关闭。</p>
        )}
      </div>
      <ToggleSwitch label="启用代理" checked={enabled} onChange={setEnabled} disabled={busy} />
      {proxy && (
        <>
          <p className={styles.note}>
            已确认固定公网IP：{proxy.fixed_exit_ip || '尚未确认'}。观测到IP不等于固定出口。
          </p>
          <ToggleSwitch
            label={`明确确认固定公网IP：${proxy.observed_exit_ip || '未知'}`}
            checked={confirmFixedIP}
            onChange={setConfirmFixedIP}
            disabled={
              busy ||
              !proxy.healthy ||
              proxy.observation_status !== 'ok' ||
              !proxy.observed_exit_ip ||
              !!url.trim() ||
              fixedConflict
            }
          />
          <p className={styles.note}>
            仅在你确认该代理出口固定时勾选；同一公网IP的入口共用归属与累计计数。替换URL后需重新健康重验，再确认固定IP。
          </p>
          {(proxy.account_id || hasManualReferences) && (
            <p className={styles.note}>
              存在手工归属或指定出口/主偏好引用；请先在对应账号解除并保存，再用新版本确认固定IP。同次提交不合并解除与确认。
            </p>
          )}
        </>
      )}
      <p className={styles.note}>启用不等于健康；有在途请求时变更可能被拒绝。</p>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      <div className={styles.actions}>
        <Button
          type="button"
          variant="secondary"
          onClick={() => {
            setUrl('');
            onClose();
          }}
        >
          取消
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? '正在提交' : proxy ? '保存代理' : '增加代理'}
        </Button>
      </div>
    </form>
  );
}

export function KiloCapacityForm({
  limit,
  busy,
  onSave,
  onClose,
}: {
  limit: number;
  busy: boolean;
  onSave: (value: number) => Promise<void>;
  onClose: () => void;
}) {
  const [value, setValue] = useState(String(limit));
  const [error, setError] = useState('');
  return (
    <form
      className={styles.form}
      onSubmit={async (event) => {
        event.preventDefault();
        if (busy) return;
        if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 100) {
          setError('每代理容量应为1至100的整数。');
          return;
        }
        await onSave(Number(value));
      }}
    >
      <Input
        label="每代理并发容量"
        type="number"
        min={1}
        max={100}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        disabled={busy}
      />
      <p className={styles.note}>默认10；下调不打断在途请求。账号并发限制独立保留。</p>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="button" variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={busy}>
          保存容量
        </Button>
      </div>
    </form>
  );
}

export function KiloEgressForm({
  account,
  snapshot,
  busy,
  onSave,
  onClose,
}: {
  account: KiloAccount;
  snapshot: KiloProxyState;
  busy: boolean;
  onSave: (input: KiloEgressInput) => Promise<void>;
  onClose: () => void;
}) {
  const existing = snapshot.accounts.find((a) => a.account_id === account.id);
  const [value, setValue] = useState<KiloEgressInput>({
    capacity_mode: existing?.capacity_mode ?? 'fixed',
    allocation_mode: existing?.allocation_mode ?? 'auto',
    max_ips: existing?.max_ips ?? 1,
    specified_ips: existing?.specified_ips ?? [],
    mode: existing?.mode ?? 'pool',
    candidate_proxy_ids: existing?.candidate_proxy_ids ?? null,
    exclusive_proxy_ids:
      existing?.mode === 'auto'
        ? []
        : snapshot.proxies
            .filter((proxy) => proxy.account_id === account.id)
            .map((proxy) => proxy.id),
    primary_proxy_id: existing?.mode === 'auto' ? '' : (existing?.primary_proxy_id ?? ''),
  });
  const [error, setError] = useState('');
  return (
    <form
      className={styles.form}
      onSubmit={async (event) => {
        event.preventDefault();
        if (busy) return;
        const validationError = validateKiloEgressInput(value);
        if (validationError) {
          setError(validationError);
          return;
        }
        setError('');
        await onSave(value);
      }}
    >
      <h3>{account.name || account.id}</h3>
      <KiloEgressFields
        value={value}
        onChange={setValue}
        snapshot={snapshot}
        accountID={account.id}
        disabled={busy}
      />
      {error && <p role="alert">{error}</p>}
      <div className={styles.actions}>
        <Button type="button" variant="secondary" onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={busy}>
          保存出口
        </Button>
      </div>
    </form>
  );
}
