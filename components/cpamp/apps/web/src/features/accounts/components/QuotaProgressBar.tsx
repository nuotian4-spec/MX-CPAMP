import styles from './QuotaProgressBar.module.scss';

export interface QuotaProgressBarProps {
  percent: number | null;
  highThreshold: number;
  mediumThreshold: number;
}

export function QuotaProgressBar({
  percent,
  highThreshold,
  mediumThreshold,
}: QuotaProgressBarProps) {
  const normalized =
    typeof percent === 'number' && Number.isFinite(percent)
      ? Math.min(100, Math.max(0, percent))
      : null;
  const tone =
    normalized === null
      ? 'unknown'
      : normalized > highThreshold
        ? 'high'
        : normalized >= mediumThreshold
          ? 'medium'
          : 'low';
  const fillClass =
    tone === 'high'
      ? styles.fillHigh
      : tone === 'medium'
        ? styles.fillMedium
        : tone === 'low'
          ? styles.fillLow
          : styles.fillUnknown;
  return (
    <span
      className={`${styles.track} ${normalized === 0 ? styles.empty : ''} ${normalized === null ? styles.unknown : ''}`}
      data-quota-tone={tone}
      role="img"
      aria-label={
        normalized === null ? '额度未知' : normalized === 0 ? '已用尽 / 0%' : `剩余 ${normalized}%`
      }
    >
      {normalized !== null && (
        <span className={`${styles.fill} ${fillClass}`} style={{ width: `${normalized}%` }} />
      )}
    </span>
  );
}
