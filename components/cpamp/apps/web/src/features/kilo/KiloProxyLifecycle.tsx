import { Button } from '@/components/ui/Button';
import { KiloModal as Modal } from './KiloModal';
import type { KiloProxy, KiloRecycledProxy } from '@/services/api/kiloProxies';
import styles from './KiloAccountsPage.module.scss';
import { kiloRetirementReason } from '@/services/api/kiloProxies';

export type KiloProxyAction =
  | { kind: 'archive' | 'recheck'; proxy: KiloProxy }
  | { kind: 'restore'; proxy: KiloRecycledProxy }
  | { kind: 'clear' };

const explanations = {
  archive: {
    title: '确认归档代理',
    note: '代理将从活跃池移入回收站，相关主偏好与出口关联会清除。存在在途请求时会拒绝归档；停用与归档是不同操作。',
    confirm: '确认归档',
    pending: '正在归档',
  },
  restore: {
    title: '检测并恢复代理',
    note: '先强制进行最长10秒的IP健康检测。普通代理通过后恢复到共享池；固定IP保留累计历史，按自动分配与累计上限规则恢复到自动独占池。保留ID、名称和顺序，旧专属关系、占用和连接均不恢复；检测失败会留在回收站。',
    confirm: '检测并恢复',
    pending: '正在检测并恢复',
  },
  clear: {
    title: '确认清空回收站',
    note: '仅永久清除回收站记录，不影响活跃代理池。清空后这些记录不再支持恢复。',
    confirm: '确认清空',
    pending: '正在清空',
  },
  recheck: {
    title: '强制健康重验',
    note: '重新进行最长10秒的IP健康检测，不使用健康缓存，不调用模型。只更新运行健康状态，不启用已停用代理，也不改变配置版本。',
    confirm: '开始重验',
    pending: '正在强制健康重验',
  },
};

export function KiloProxyActionDialog({
  action,
  busy,
  onClose,
  onConfirm,
}: {
  action: KiloProxyAction;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const copy =
    action.kind === 'restore'
      ? {
          ...explanations.restore,
          title: action.proxy.fixed_exit_ip ? '恢复到自动独占池' : '恢复到共享池',
          note: action.proxy.fixed_exit_ip
            ? '先强制进行最长10秒的IP健康检测；通过后按自动分配与累计上限规则恢复到自动独占池。保留ID、名称、顺序和累计历史，旧归属与占用不恢复；检测失败会留在回收站。'
            : '先强制进行最长10秒的IP健康检测，通过后才启用并恢复到共享池。保留ID、名称和顺序，旧专属关系、占用和连接均不恢复；检测失败会留在回收站。',
        }
      : explanations[action.kind];
  return (
    <Modal open title={copy.title} onClose={onClose}>
      <div className={styles.form}>
        {'proxy' in action && <strong>{action.proxy.name || action.proxy.id}</strong>}
        <p className={styles.note}>{copy.note}</p>
        {action.kind === 'restore' && (
          <p className={styles.note}>
            固定IP还须无当前归属且累计不同账号数低于N；提高N不自动复活已退休IP。恢复不清累计历史。
          </p>
        )}
        {busy && <p role="status">{copy.pending}；取消请求后需刷新核对结果。</p>}
        <div className={styles.actions}>
          <Button variant="secondary" onClick={onClose}>
            {busy ? '取消请求' : '取消'}
          </Button>
          <Button disabled={busy} onClick={() => void onConfirm()}>
            {busy ? copy.pending : copy.confirm}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

export function KiloRecycleTable({
  records,
  blocked,
  accountLabel,
  onAction,
}: {
  records: KiloRecycledProxy[];
  blocked: boolean;
  accountLabel: (id: string) => string;
  onAction: (action: KiloProxyAction) => void;
}) {
  return (
    <section aria-label="代理回收站">
      <div className={styles.header}>
        <div>
          <h2>代理回收站</h2>
          <p>
            原归属仅用于记录；恢复前重新检测健康。普通代理通过后回到共享池；固定IP保留累计历史，按自动分配与累计上限规则恢复。
          </p>
        </div>
        <Button
          variant="secondary"
          disabled={blocked || records.length === 0}
          onClick={() => onAction({ kind: 'clear' })}
        >
          清空回收站
        </Button>
      </div>
      <div className={styles.tableScroll}>
        <table className={styles.table}>
          <caption className={styles.caption}>回收站 · {records.length}条</caption>
          <thead>
            <tr>
              <th>代理</th>
              <th>归档前配置 / 原归属</th>
              <th>归档时间（UTC）</th>
              <th className={styles.number}>顺序</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {records.map((p) => (
              <tr key={p.id}>
                <td>
                  <strong>{p.name || p.id}</strong>
                  <small>
                    {p.scheme} · {p.host} {p.port || '默认端口'} ·{' '}
                    {p.has_auth ? '有认证' : '无认证'}
                  </small>
                </td>
                <td>
                  {p.enabled ? '已启用' : '已停用'}
                  {p.fixed_exit_ip && <small>固定IP：{p.fixed_exit_ip}</small>}
                  {p.retirement_reason && (
                    <small>退役原因：{kiloRetirementReason[p.retirement_reason]}</small>
                  )}
                  <small>
                    {p.fixed_exit_ip
                      ? '自动独占池（历史见IP归组）'
                      : p.original_account_id
                        ? `原专属：${accountLabel(p.original_account_id)}`
                        : '原共享'}
                  </small>
                </td>
                <td>
                  <time dateTime={p.archived_at}>{p.archived_at}</time>
                </td>
                <td className={styles.number}>{p.order}</td>
                <td>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={blocked}
                    onClick={() => onAction({ kind: 'restore', proxy: p })}
                  >
                    恢复代理
                  </Button>
                </td>
              </tr>
            ))}
            {records.length === 0 && (
              <tr>
                <td colSpan={5}>回收站为空。</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
