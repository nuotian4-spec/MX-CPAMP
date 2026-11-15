import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import type { KiloEgressInput, KiloProxyState } from '@/services/api/kiloProxies';
import styles from './KiloAccountsPage.module.scss';

export function KiloEgressFields({
  value,
  onChange,
  snapshot,
  accountID = '',
  disabled,
}: {
  value: KiloEgressInput;
  onChange: (value: KiloEgressInput) => void;
  snapshot: KiloProxyState;
  accountID?: string;
  disabled: boolean;
}) {
  const mode =
    value.mode === 'auto'
      ? 'auto'
      : value.mode === 'direct'
        ? 'direct'
        : value.candidate_proxy_ids
          ? 'selected'
          : 'pool';
  const available = snapshot.proxies.filter(
    (proxy) => !proxy.fixed_exit_ip && (!proxy.account_id || proxy.account_id === accountID)
  );
  const candidates = value.candidate_proxy_ids ?? [];
  return (
    <section className={styles.form} aria-label="保存后账号出口">
      <Select
        ariaLabel="出口策略"
        value={mode}
        disabled={disabled}
        options={[
          { value: 'auto', label: '独占固定公网 IP' },
          { value: 'pool', label: '共享池自动分配' },
          { value: 'selected', label: '指定多个出口' },
          { value: 'direct', label: '直连' },
        ]}
        onChange={(next) =>
          onChange({
            capacity_mode: next === 'auto' ? 'ip' : 'fixed',
            allocation_mode: 'auto',
            max_ips: 1,
            specified_ips: [],
            mode: next === 'direct' ? 'direct' : next === 'auto' ? 'auto' : 'pool',
            candidate_proxy_ids: next === 'selected' ? [] : null,
            exclusive_proxy_ids: [],
            primary_proxy_id: '',
          })
        }
      />
      {mode === 'auto' && (
        <>
          <Select
            ariaLabel="并发模式"
            value={value.capacity_mode ?? 'fixed'}
            disabled={disabled}
            options={[
              { value: 'ip', label: '按 IP 容量扩展' },
              { value: 'fixed', label: '固定并发 · 单 IP' },
            ]}
            onChange={(capacity_mode) =>
              onChange({
                ...value,
                capacity_mode: capacity_mode as 'fixed' | 'ip',
                allocation_mode: 'auto',
                max_ips: 1,
                specified_ips: [],
              })
            }
          />
          {value.capacity_mode === 'ip' && (
            <>
              <Input
                label="最多分配 IP 数"
                type="number"
                min={1}
                step={1}
                value={String(value.max_ips ?? 1)}
                disabled={disabled}
                onChange={(event) => onChange({ ...value, max_ips: Number(event.target.value) })}
              />
              <p className={styles.note}>
                配置上界 {(value.max_ips ?? 1) * (snapshot.ip_concurrency_limit ?? 20)} 并发 · 每 IP{' '}
                {snapshot.ip_concurrency_limit ?? 20}。人工账号低限额与入口限制继续生效。
              </p>
              <Select
                ariaLabel="IP 分配方式"
                value={value.allocation_mode ?? 'auto'}
                disabled={disabled}
                options={[
                  { value: 'auto', label: '自动按需扩容' },
                  { value: 'specified', label: '人工指定 IP 集合' },
                ]}
                onChange={(allocation_mode) =>
                  onChange({
                    ...value,
                    allocation_mode: allocation_mode as 'auto' | 'specified',
                    specified_ips: [],
                  })
                }
              />
              {value.allocation_mode === 'specified' && (
                <div className={styles.choiceList}>
                  {(snapshot.ip_groups ?? [])
                    .filter((group) => !group.retired)
                    .map((group) => (
                      <label key={group.ip} className={styles.choiceRow}>
                        <input
                          type="checkbox"
                          checked={(value.specified_ips ?? []).includes(group.ip)}
                          disabled={
                            disabled ||
                            (!!group.owner_account_id && group.owner_account_id !== accountID)
                          }
                          onChange={(event) =>
                            onChange({
                              ...value,
                              specified_ips: event.target.checked
                                ? [...(value.specified_ips ?? []), group.ip]
                                : (value.specified_ips ?? []).filter((ip) => ip !== group.ip),
                            })
                          }
                        />
                        <span>
                          {group.ip}
                          <small>
                            {group.owner_account_id && group.owner_account_id !== accountID
                              ? '已归属其他账号'
                              : `${group.proxy_ids.length} 个入口 · 累计 ${group.unique_accounts}/${group.max_unique_accounts}`}
                          </small>
                        </span>
                      </label>
                    ))}
                  {!snapshot.ip_groups?.length && <p>尚无已确认的固定公网 IP。</p>}
                </div>
              )}
              <p className={styles.note}>
                {value.allocation_mode === 'specified'
                  ? '保存时整组配齐；发生争抢时保留原配置，新账号保存凭证并等待指定 IP。'
                  : '先分配一个 IP，满载时按需增加，最多达到配置数量。排空中的 IP 仍占名额。'}
              </p>
            </>
          )}
        </>
      )}
      {mode === 'selected' && (
        <div className={styles.choiceList}>
          {available.map((proxy) => (
            <div key={proxy.id} className={styles.choiceRow}>
              <label>
                <input
                  type="checkbox"
                  disabled={disabled}
                  checked={candidates.includes(proxy.id)}
                  onChange={(event) => {
                    const next = event.target.checked
                      ? [...candidates, proxy.id]
                      : candidates.filter((id) => id !== proxy.id);
                    onChange({
                      ...value,
                      candidate_proxy_ids: next,
                      exclusive_proxy_ids: value.exclusive_proxy_ids.filter((id) =>
                        next.includes(id)
                      ),
                      primary_proxy_id: next.includes(value.primary_proxy_id)
                        ? value.primary_proxy_id
                        : '',
                    });
                  }}
                />{' '}
                {proxy.name || proxy.id}
                <small>
                  入口 {proxy.host}
                  {proxy.port && `:${proxy.port}`} · 公网 {proxy.observed_exit_ip || '未知'} ·{' '}
                  {proxy.account_id ? '本账号专属' : '共享'}
                </small>
              </label>
              {candidates.includes(proxy.id) && (
                <ToggleSwitch
                  label="独占"
                  checked={value.exclusive_proxy_ids.includes(proxy.id)}
                  disabled={disabled}
                  onChange={(checked) =>
                    onChange({
                      ...value,
                      exclusive_proxy_ids: checked
                        ? [...value.exclusive_proxy_ids, proxy.id]
                        : value.exclusive_proxy_ids.filter((id) => id !== proxy.id),
                    })
                  }
                />
              )}
            </div>
          ))}
          {available.length === 0 && <p>没有可分配出口，请先添加代理。</p>}
        </div>
      )}
      {mode !== 'auto' && mode !== 'direct' && (
        <p className={styles.note}>
          已确认固定公网IP的代理属于自动独占池，不参与手工指定或主偏好。
        </p>
      )}
      {mode !== 'direct' && mode !== 'auto' && (
        <Select
          ariaLabel="主偏好"
          value={value.primary_proxy_id}
          disabled={disabled}
          options={[
            { value: '', label: '主偏好：自动选择' },
            ...available
              .filter((proxy) => mode !== 'selected' || candidates.includes(proxy.id))
              .map((proxy) => ({ value: proxy.id, label: proxy.name || proxy.id })),
          ]}
          onChange={(primary_proxy_id) => onChange({ ...value, primary_proxy_id })}
        />
      )}
      <p className={styles.note}>
        {mode === 'auto'
          ? '固定公网 IP 按账号独占，缺少可用 IP 时等待。凭证保存与出口配齐分别确认。'
          : mode === 'selected'
            ? '只使用选中的出口；勾选共享出口保持共享，独占需单独开启。'
            : mode === 'direct'
              ? '直连不使用代理；保存将释放本账号专属出口。'
              : '按代理池健康状态和容量自动分配共享及本账号专属出口。'}
      </p>
    </section>
  );
}
