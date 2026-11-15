import { QuotaProgressBar } from './QuotaProgressBar';
import {
  formatPercent,
  formatQuotaResetDisplay,
  type AccountQuotaLifecycleBarOverride,
  type AntigravityQuotaMatrix,
} from '@/features/accounts/model/accountsPagePresentation';
import { useTranslation } from 'react-i18next';
import styles from '../AccountsPage.module.scss';

interface AccountQuotaMatrixProps {
  accountKey: string;
  matrix: AntigravityQuotaMatrix;
  lifecycleBarOverride?: AccountQuotaLifecycleBarOverride;
}

export function AccountQuotaMatrix({
  accountKey,
  matrix,
  lifecycleBarOverride: _lifecycleBarOverride = null,
}: AccountQuotaMatrixProps) {
  const { t, i18n } = useTranslation();
  return (
    <span className={styles.quotaMatrix} data-account-quota-matrix={accountKey}>
      {matrix.rows.map((matrixRow) => (
        <span
          key={matrixRow.key}
          className={styles.quotaMatrixRow}
          data-account-quota-matrix-row={matrixRow.key}
        >
          <span className={styles.quotaMatrixWindowLabel}>{matrixRow.label}</span>
          <span className={styles.quotaMatrixCells}>
            {matrixRow.cells.map((cell) => {
              const windowRemaining = cell.window.remainingPercent;
              const resetDisplay = formatQuotaResetDisplay(
                cell.window.resetAtMs,
                cell.window.resetLabel,
                i18n.language
              );
              const title = [
                `${cell.groupLabel} ${cell.window.label}: ${formatPercent(windowRemaining)}`,
                resetDisplay !== '-' ? `${t('accounts.col_reset')}: ${resetDisplay}` : '',
              ]
                .filter(Boolean)
                .join(' · ');
              return (
                <span
                  key={cell.window.key}
                  className={styles.quotaMatrixCell}
                  data-account-quota-matrix-cell={`${matrixRow.key}:${cell.groupLabel}`}
                  title={title}
                >
                  <span className={styles.quotaMatrixGroupLabel} title={cell.groupLabel}>
                    {cell.displayLabel}
                  </span>
                  <QuotaProgressBar
                    percent={windowRemaining}
                    highThreshold={50}
                    mediumThreshold={20}
                  />
                  <strong className={styles.quotaMatrixPercent}>
                    {typeof windowRemaining !== 'number' || !Number.isFinite(windowRemaining)
                      ? '未知'
                      : windowRemaining === 0
                        ? '已用尽 / 0%'
                        : formatPercent(windowRemaining)}
                  </strong>
                </span>
              );
            })}
          </span>
        </span>
      ))}
    </span>
  );
}
