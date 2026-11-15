import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Drawer } from '@/components/ui/Drawer';
import { Button } from '@/components/ui/Button';
import { DropdownMenu } from '@/components/ui/DropdownMenu';
import {
  kiloRetirementReason,
  kiloAdmissionReason,
  type KiloIPGroup,
  type KiloProxy,
  type KiloProxyState,
  type KiloRecycledProxy,
} from '@/services/api/kiloProxies';
import type { KiloProxyAction } from './KiloProxyLifecycle';
import styles from './KiloProxiesPage.module.scss';

export interface KiloIPRelation {
  key: string;
  ip: string;
  group?: KiloIPGroup;
  entries: KiloProxy[];
  archivedEntries: KiloRecycledProxy[];
  archived: boolean;
}
export function kiloIPRelations(snapshot: KiloProxyState): KiloIPRelation[] {
  const rows = new Map<string, KiloIPRelation>();
  const ensure = (key: string, ip: string, archived: boolean) => {
    let row = rows.get(key);
    if (!row) {
      row = { key, ip, entries: [], archivedEntries: [], archived };
      rows.set(key, row);
    }
    return row;
  };
  for (const group of snapshot.ip_groups ?? []) {
    ensure(`ip:${group.ip}`, group.ip, group.retired).group = group;
  }
  for (const proxy of snapshot.proxies) {
    const group = snapshot.ip_groups?.find((item) => item.proxy_ids.includes(proxy.id));
    const ip =
      proxy.admitted_ip ||
      group?.ip ||
      (proxy.admission_status === 'pending'
        ? ''
        : proxy.fixed_exit_ip || proxy.observed_exit_ip || '');
    const row = ensure(ip ? `ip:${ip}` : `unknown:${proxy.id}`, ip, false);
    row.entries.push(proxy);
    row.archived = false;
  }
  for (const proxy of snapshot.recycle) {
    const ip = proxy.admitted_ip || proxy.fixed_exit_ip;
    if (!ip) continue;
    ensure(`ip:${ip}`, ip, true).archivedEntries.push(proxy);
  }
  return [...rows.values()];
}
function hasSharedEntries(row: KiloIPRelation) {
  return !!row.group && row.entries.some((entry) => !row.group!.proxy_ids.includes(entry.id));
}
function fixedGroupPending(row: KiloIPRelation) {
  return !row.group && row.entries.some((entry) => !!entry.fixed_exit_ip);
}
function relationLabel(row: KiloIPRelation) {
  if (row.ip) return row.ip;
  const entry = row.entries[0];
  if (!entry) return '待识别入口';
  return entry.name || `${entry.scheme}://${entry.host}${entry.port ? `:${entry.port}` : ''}`;
}
function capacityLabel(row: KiloIPRelation) {
  if (row.entries.length && row.entries.every((p) => p.admission_status === 'pending'))
    return '待核不贡献容量';
  if (!row.group) return '未知 · 按入口限额';
  return `固定池 ${row.group.capacity ?? '未知'}${hasSharedEntries(row) ? ' · 共享入口另按逐入口限额' : ''}`;
}
function cumulativeLabel(row: KiloIPRelation) {
  return row.group
    ? `固定池 ${row.group.unique_accounts} / ${row.group.max_unique_accounts || '未配置'}`
    : fixedGroupPending(row)
      ? '未知 · 固定池累计待核'
      : '未知';
}
function cumulativeNote(row: KiloIPRelation) {
  return row.group
    ? hasSharedEntries(row)
      ? '共享入口累计未知'
      : '固定池不同账号累计'
    : fixedGroupPending(row)
      ? '固定IP归属与累计待核'
      : '共享模式不套用固定池累计上限';
}
const states = {
  available: '可分配',
  assigned: '已独占',
  retiring: '归还后退役',
  draining: '正在排空',
  retired: '已退休',
  unverified: '待核固定 IP',
  at_limit: '已达累计上限',
};
const observation = {
  unknown: '未检测',
  pending: '检测中',
  ok: '已观测',
  failed: '检测失败',
  stale: '观测陈旧',
};
const accountHref = (id: string) => `/kilo-accounts?account=${encodeURIComponent(id)}`;
const sum = (values: (number | undefined)[]) =>
  values.every((v) => v !== undefined)
    ? values.reduce<number>((total, n) => total + n!, 0)
    : undefined;
function activity(row: KiloIPRelation, kind: 'inference_active' | 'management_active') {
  if (!row.group) return sum(row.entries.map((p) => p[kind]));
  return sum([
    row.group[kind],
    ...row.entries.filter((p) => !row.group!.proxy_ids.includes(p.id)).map((p) => p[kind]),
  ]);
}
function ownerIDs(row: KiloIPRelation) {
  return [
    ...new Set(
      [row.group?.owner_account_id, ...row.entries.map((p) => p.account_id)].filter(
        (id): id is string => !!id
      )
    ),
  ];
}
function status(row: KiloIPRelation) {
  if (row.entries.some((p) => p.admission_status === 'pending')) return '已保存待核 · 不入池';
  if (row.group) return states[row.group.state];
  if (row.archived) return '已归档';
  if (!row.ip) return '公网 IP 待识别';
  return row.entries.some((p) => p.observation_status !== 'ok') ? '观测待核' : '已观测';
}
export function KiloIPGroups({
  snapshot,
  accountLabel,
  accountID = '',
  filter = 'all',
  range = 'all',
  search = '',
  recycled = false,
  blocked = false,
  onEdit,
  onAction,
  onEgress,
  focusProxyID,
  onFocusHandled,
}: {
  snapshot: KiloProxyState;
  accountLabel: (id: string) => string;
  accountID?: string;
  filter?: string;
  range?: string;
  search?: string;
  recycled?: boolean;
  blocked?: boolean;
  onEdit?: (proxy: KiloProxy) => void;
  onAction?: (action: KiloProxyAction) => void;
  onEgress?: (id: string) => void;
  focusProxyID?: string;
  onFocusHandled?: () => void;
}) {
  const [selected, setSelected] = useState('');
  const all = kiloIPRelations(snapshot);
  const matchesAccount = (row: KiloIPRelation, id: string) =>
    ownerIDs(row).includes(id) ||
    row.entries.some(
      (p) =>
        (p.active > 0 && p.owner_kind !== 'device' && p.owner === id) ||
        snapshot.accounts.some(
          (a) =>
            a.account_id === id &&
            (a.candidate_proxy_ids?.includes(p.id) || a.primary_proxy_id === p.id)
        )
    );
  const available = (row: KiloIPRelation) =>
    !!row.ip &&
    !row.archived &&
    row.entries.some((p) => p.admission_status !== 'pending' && p.enabled && p.healthy) &&
    (!row.group || row.group.state === 'available');
  const rows = all.filter((row) => {
    if (row.archived !== (recycled || range === 'archived')) return false;
    if ((range === 'known' && !row.ip) || (range === 'unknown' && row.ip)) return false;
    if (accountID && !matchesAccount(row, accountID)) return false;
    if (
      search &&
      !`${row.ip} ${row.entries.map((p) => `${p.id} ${p.name} ${p.admitted_ip ?? ''} ${p.observed_exit_ip ?? ''}`).join(' ')} ${ownerIDs(row).map(accountLabel).join(' ')}`
        .toLowerCase()
        .includes(search.toLowerCase().trim())
    )
      return false;
    return (
      filter === 'all' ||
      (filter === 'shared' && !ownerIDs(row).length) ||
      (filter === 'exclusive' && !!ownerIDs(row).length) ||
      (filter === 'active' && row.entries.some((p) => p.active > 0)) ||
      (filter === 'unhealthy' && row.entries.some((p) => !p.healthy)) ||
      (filter === 'pending' && row.entries.some((p) => p.admission_status === 'pending')) ||
      (filter === 'registered' && row.entries.some((p) => !!p.admitted_ip)) ||
      (filter === 'available' && available(row))
    );
  });
  const detail = all.find((row) =>
    focusProxyID
      ? row.entries.some((p) => p.id === focusProxyID) ||
        row.archivedEntries.some((p) => p.id === focusProxyID)
      : row.key === selected
  );
  const accountLink = (id: string) => (
    <Link key={id} to={accountHref(id)}>
      {accountLabel(id)}
    </Link>
  );
  const ownership = (row: KiloIPRelation) =>
    ownerIDs(row).length
      ? ownerIDs(row).map(accountLink)
      : row.group
        ? '无当前归属'
        : fixedGroupPending(row)
          ? '固定IP归属待核'
          : '共享池';
  const finishDetail = (action: () => void) => {
    setSelected('');
    onFocusHandled?.();
    action();
  };
  const renderEntry = (proxy: KiloProxy) => (
    <article key={proxy.id} className={styles.entry}>
      <div className={styles.entryHead}>
        <strong>{proxy.name || proxy.id}</strong>
        <DropdownMenu
          ariaLabel={`${proxy.name || proxy.id}代理操作`}
          triggerLabel="管理"
          disabled={blocked}
          items={[
            { key: 'edit', label: '编辑代理', onClick: () => finishDetail(() => onEdit?.(proxy)) },
            {
              key: 'check',
              label: '健康与公网IP重验',
              onClick: () => finishDetail(() => onAction?.({ kind: 'recheck', proxy })),
            },
            {
              key: 'archive',
              label: '归档代理',
              onClick: () => finishDetail(() => onAction?.({ kind: 'archive', proxy })),
            },
          ]}
        />
      </div>
      <code>
        {proxy.scheme}://{proxy.has_auth ? '***@' : ''}
        {proxy.host}
        {proxy.port ? `:${proxy.port}` : ''}
      </code>
      <small>
        {proxy.enabled ? '已启用' : '已停用'} · {proxy.healthy ? '健康' : '未健康'} · 顺序{' '}
        {proxy.order}
      </small>
      <small>
        推理 {proxy.inference_active ?? '未知'} · 管理 {proxy.management_active ?? '未知'} ·
        入口限额 {proxy.effective_limit ?? snapshot.proxy_limit}
      </small>
      <small>
        {observation[proxy.observation_status ?? 'unknown']} ·{' '}
        {proxy.observed_at ? new Date(proxy.observed_at).toLocaleString() : '观测时间未知'} ·
        固定IP确认：{proxy.fixed_exit_ip || '未确认'}
      </small>
      <small>
        {proxy.admission_status === 'pending'
          ? `已保存待核 · ${kiloAdmissionReason[proxy.admission_reason ?? 'unverified']} · 不入池、不增加容量`
          : proxy.admission_status === 'admitted'
            ? '已登记 · 是否分配仍按当前健康与归属判断'
            : '旧版兼容 · 准入记录未知'}
      </small>
      <small>
        登记IP：{proxy.admitted_ip || '未知'} · 最近接纳：
        {proxy.admitted_at ? new Date(proxy.admitted_at).toLocaleString() : '未知'}
      </small>
      <small>
        当前观测IP：{proxy.observed_exit_ip || '未知'}
        {proxy.admitted_ip && proxy.observed_exit_ip && proxy.admitted_ip !== proxy.observed_exit_ip
          ? ' · 与登记IP不同'
          : ''}
      </small>
      <small>
        指定候选：
        {snapshot.accounts
          .filter((a) => a.mode !== 'direct' && a.candidate_proxy_ids?.includes(proxy.id))
          .map((a) => accountLink(a.account_id))}{' '}
        {!snapshot.accounts.some(
          (a) => a.mode !== 'direct' && a.candidate_proxy_ids?.includes(proxy.id)
        ) && '无'}
      </small>
      <small>
        主偏好：
        {snapshot.accounts
          .filter((a) => a.primary_proxy_id === proxy.id)
          .map((a) => accountLink(a.account_id))}
        {!snapshot.accounts.some((a) => a.primary_proxy_id === proxy.id) && '无'}
      </small>
    </article>
  );
  return (
    <section aria-label="公网IP关系">
      <div className={styles.summary}>
        <div>
          <b>
            {
              new Set(
                [...snapshot.proxies, ...snapshot.recycle].map((p) => p.admitted_ip).filter(Boolean)
              ).size
            }
          </b>
          <span>已登记 IP（当前记录）</span>
        </div>
        <div>
          <b>{all.filter(available).length}</b>
          <span>当前可分配 IP</span>
        </div>
        <div>
          <b>{snapshot.proxies.filter((p) => p.admission_status === 'pending').length}</b>
          <span>待核条目</span>
        </div>
        <div>
          <b>{rows.length}</b>
          <span>
            {range === 'unknown'
              ? '待识别入口'
              : range === 'archived'
                ? '归档 IP'
                : '公网 IP / 待识别'}
          </span>
        </div>
        <div>
          <b>
            {rows.reduce((total, row) => total + row.entries.reduce((n, p) => n + p.active, 0), 0)}
          </b>
          <span>当前请求</span>
        </div>
        <div>
          <b>{rows.filter((row) => ownerIDs(row).length).length}</b>
          <span>独占归属</span>
        </div>
      </div>
      {snapshot.auto_error && <p role="alert">自动分配状态保存待核，请刷新确认。</p>}
      <div className={styles.tableScroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              {['公网 IP', '当前归属', '正在使用', '累计不同账号', '状态', '代理入口', '操作'].map(
                (text) => (
                  <th key={text} scope="col">
                    {text}
                  </th>
                )
              )}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key}>
                <td>
                  <button className={styles.link} onClick={() => setSelected(row.key)}>
                    {relationLabel(row)}
                  </button>
                  <small>
                    {row.group
                      ? '固定公网 IP'
                      : row.entries.some((p) => p.fixed_exit_ip)
                        ? '固定IP归属待核'
                        : row.ip
                          ? '实测公网出口 · 非固定确认'
                          : '公网 IP 未知'}
                  </small>
                </td>
                <td>
                  {ownership(row)}
                  <small>{row.group ? '自动独占池' : '逐入口关系'}</small>
                </td>
                <td>
                  <strong>
                    推理 {activity(row, 'inference_active') ?? '未知'} · 管理{' '}
                    {activity(row, 'management_active') ?? '未知'}
                  </strong>
                  <small>{row.group ? capacityLabel(row) : '容量按入口，IP统一上限未知'}</small>
                  {ownerIDs(row).length > 0 && row.entries.every((p) => p.active === 0) && (
                    <small>归属保留 · 当前空闲</small>
                  )}
                </td>
                <td>
                  {cumulativeLabel(row)}
                  <small>{cumulativeNote(row)}</small>
                </td>
                <td>
                  <span className={styles.tag}>{status(row)}</span>
                  {!!row.entries.length && (
                    <small>
                      {row.entries.every((p) => !p.enabled) ? '已停用' : '已启用入口'} ·{' '}
                      {row.entries.some((p) => !p.healthy) ? '有入口未健康' : '入口健康'}
                    </small>
                  )}
                  {row.group?.reason && <small>{kiloRetirementReason[row.group.reason]}</small>}
                </td>
                <td>
                  {row.entries.length + row.archivedEntries.length} 个入口
                  <small>{row.ip ? '同 IP 归组' : '独立入口 · 公网IP待识别'}</small>
                </td>
                <td>
                  <Button variant="secondary" size="sm" onClick={() => setSelected(row.key)}>
                    详情
                  </Button>
                </td>
              </tr>
            ))}
            {!rows.length && (
              <tr>
                <td colSpan={7}>暂无符合条件的出口。</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className={styles.note}>
        0 请求不解除独占归属；累计不同账号数与当前请求并发分别记录。代理入口与操作在详情内。
      </p>
      {detail && (
        <Drawer
          open
          title={relationLabel(detail)}
          width={610}
          className={styles.drawer}
          onClose={() => {
            setSelected('');
            onFocusHandled?.();
          }}
          footer={
            <div className={styles.actions}>
              {ownerIDs(detail).map((id) => (
                <Button
                  key={id}
                  variant="secondary"
                  disabled={blocked}
                  onClick={() => finishDetail(() => onEgress?.(id))}
                >
                  配置 {accountLabel(id)} 出口
                </Button>
              ))}
              <Button
                variant="secondary"
                onClick={() => {
                  setSelected('');
                  onFocusHandled?.();
                }}
              >
                关闭详情
              </Button>
            </div>
          }
        >
          <section className={styles.detailSection}>
            <h3>归属与容量</h3>
            <div className={styles.detailRow}>
              <span>当前归属</span>
              <strong>{ownership(detail)}</strong>
            </div>
            <div className={styles.detailRow}>
              <span>状态</span>
              <span>{status(detail)}</span>
            </div>
            <div className={styles.detailRow}>
              <span>IP 容量</span>
              <span>{capacityLabel(detail)}</span>
            </div>
            <div className={styles.detailRow}>
              <span>累计不同账号 / N</span>
              <span>
                {cumulativeLabel(detail)}
                <small>{cumulativeNote(detail)}</small>
              </span>
            </div>
            {ownerIDs(detail).length > 0 && detail.entries.every((p) => p.active === 0) && (
              <p>当前 0 请求，独占归属继续保留。</p>
            )}
            {detail.group?.owner_account_id && (
              <p>调整或归还归属请在账号出口配置处理；归档代理不等于归还单个 IP。</p>
            )}
          </section>
          <section className={styles.detailSection}>
            <h3>正在使用</h3>
            <p>
              推理 {activity(detail, 'inference_active') ?? '未知'} · 管理{' '}
              {activity(detail, 'management_active') ?? '未知'}
            </p>
            {detail.entries
              .filter((p) => p.active > 0)
              .map((p) => (
                <div key={p.id} className={styles.detailRow}>
                  <span>
                    {p.owner_kind === 'device'
                      ? '设备授权会话'
                      : p.owner
                        ? accountLink(p.owner)
                        : '占用账号未知'}
                    <small>{p.name || p.id}</small>
                  </span>
                  <span>{p.active} 个请求</span>
                </div>
              ))}
            {!detail.entries.some((p) => p.active > 0) && <p>当前没有在途请求。</p>}
          </section>
          <section className={styles.detailSection}>
            <h3>代理入口 · {detail.entries.length + detail.archivedEntries.length}</h3>
            {detail.entries.map(renderEntry)}
            {detail.archivedEntries.map((p) => (
              <article key={p.id} className={styles.entry}>
                <strong>{p.name || p.id}</strong>
                <code>
                  {p.scheme}://{p.has_auth ? '***@' : ''}
                  {p.host}
                  {p.port ? `:${p.port}` : ''}
                </code>
                <small>已归档 · {p.archived_at || '时间未知'}</small>
                <small>
                  登记IP：{p.admitted_ip || '未知'} · 最近接纳：
                  {p.admitted_at ? new Date(p.admitted_at).toLocaleString() : '未知'}
                </small>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={blocked}
                  onClick={() => finishDetail(() => onAction?.({ kind: 'restore', proxy: p }))}
                >
                  恢复代理
                </Button>
              </article>
            ))}
          </section>
          <section className={styles.detailSection}>
            <h3>{detail.group ? '固定池累计分配历史' : '累计分配历史'}</h3>
            {detail.group ? (
              <>
                <ol className={styles.history}>
                  {detail.group.account_ids.map((id) => (
                    <li key={id}>
                      {accountLink(id)}
                      <small>AuthID {id} · 分配时间未知</small>
                    </li>
                  ))}
                </ol>
                {!detail.group.account_ids.length && <p>尚无历史分配。</p>}
                <p>同一账号再次使用不增加累计；归还与归档保留历史。</p>
                {hasSharedEntries(detail) && <p>共享入口累计账号历史未知。</p>}
              </>
            ) : fixedGroupPending(detail) ? (
              <p>固定IP累计账号历史待核。</p>
            ) : (
              <p>共享入口未返回IP级累计账号历史，不套用自动独占上限。</p>
            )}
          </section>
        </Drawer>
      )}
    </section>
  );
}
