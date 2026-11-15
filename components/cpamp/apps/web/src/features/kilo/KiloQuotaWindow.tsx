import { QuotaProgressBar } from '@/features/accounts/components/QuotaProgressBar';
import styles from './KiloQuotaWindow.module.scss';

export function KiloQuotaWindow({
  label,
  percent,
  resetAt,
}: {
  label: string;
  percent: number | null;
  resetAt: string | null;
}) {
  const known = typeof percent === 'number' && Number.isFinite(percent);
  const value = known ? Math.max(0, Math.min(100, percent)) : null;
  const text = value === null ? '未知' : value === 0 ? '已用尽 / 0%' : `${value}% 剩余`;
  return (
    <div className={styles.window} data-kilo-quota-window={label}>
      <div className={styles.heading}>
        <span>{label}</span>
        <strong>{text}</strong>
      </div>
      <QuotaProgressBar percent={value} highThreshold={50} mediumThreshold={20} />
      <small>{resetAt ? `${new Date(resetAt).toLocaleString()} 重置` : '重置时间未知'}</small>
    </div>
  );
}
