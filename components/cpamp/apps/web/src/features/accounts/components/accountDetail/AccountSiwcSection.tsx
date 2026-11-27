import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { AccountRow } from '@/features/accounts/model/accountRows';
import { useSiwcBinding } from '@/features/accounts/hooks/useSiwcBinding';
import { copyToClipboard } from '@/utils/clipboard';
import pageStyles from '@/features/accounts/AccountsPage.module.scss';
import styles from './AccountSiwcSection.module.scss';

export function AccountSiwcSection({
  row,
  scope,
  home,
  disabled,
}: {
  row: AccountRow;
  scope: ApiClientRequestScope;
  home: boolean;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const [callback, setCallback] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => {
    setCallback('');
    setNotice('');
  }, [row.raw.id, row.authIndex, scope.apiBase, scope.managementKey]);
  const readOnly = home || row.runtimeOnly || Boolean(row.raw.source && row.raw.source !== 'file');
  const binding = useSiwcBinding(
    { auth_id: typeof row.raw.id === 'string' ? row.raw.id : '', auth_index: row.authIndex || '' },
    scope,
    readOnly
  );
  const credential = binding.credential;
  const session = binding.session;
  const busy = ['loading', 'starting', 'submitting', 'cancelling'].includes(binding.phase);
  const canStart =
    !disabled && !readOnly && credential?.siwc_bind_supported === true && !session && !busy;
  const status = credential ? credential.siwc_status : 'unknown';
  const copy = async () => {
    if (session) setNotice((await copyToClipboard(session.url)) ? 'copied' : 'copy_failed');
  };
  return (
    <section
      className={`${pageStyles.configurationSection} ${styles.section}`}
      aria-label={t('siwc.title')}
    >
      <div className={styles.heading}>
        <div>
          <h3 className={pageStyles.configurationSectionTitle}>{t('siwc.title')}</h3>
          <p className={styles.summary}>{t('siwc.description')}</p>
        </div>
        <span className={styles.status} data-status={status}>
          {t(`siwc.status_${status}`)}
        </span>
      </div>
      {credential?.siwc_expires_at && (
        <p className={styles.summary}>
          {t('siwc.expires', { date: new Date(credential.siwc_expires_at).toLocaleString() })}
        </p>
      )}
      {(readOnly || credential?.siwc_bind_supported === false) && (
        <p className={styles.summary}>
          {t(
            home || credential?.read_only_reason === 'home_managed'
              ? 'siwc.home_readonly'
              : 'siwc.readonly'
          )}
        </p>
      )}
      <div className={styles.actions}>
        <Button
          type="button"
          size="sm"
          onClick={() => {
            setCallback('');
            setNotice('');
            void binding.start();
          }}
          disabled={!canStart}
        >
          {t(
            binding.phase === 'starting'
              ? 'siwc.starting'
              : credential?.siwc_bound
                ? 'siwc.rebind'
                : 'siwc.bind'
          )}
        </Button>
        {!session && (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            disabled={busy}
            onClick={() => void binding.refresh()}
          >
            {t('siwc.refresh')}
          </Button>
        )}
      </div>
      {session && (
        <div className={styles.session}>
          <p className={styles.summary} role="status">
            {t(binding.submitted ? 'siwc.submitted' : 'siwc.waiting')}
          </p>
          <Input label={t('siwc.authorize_url')} value={session.url} readOnly autoComplete="off" />
          <div className={styles.actions}>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => {
                const popup = window.open(session.url, '_blank');
                if (popup) {
                  popup.opener = null;
                  setNotice('');
                } else setNotice('popup_blocked');
              }}
            >
              {t('siwc.open')}
            </Button>
            <Button type="button" size="sm" variant="secondary" onClick={() => void copy()}>
              {t('siwc.copy')}
            </Button>
          </div>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const value = callback;
              setCallback('');
              void binding.submit(value);
            }}
          >
            <Input
              label={t('siwc.callback')}
              hint={t('siwc.callback_hint')}
              value={callback}
              onChange={(event) => setCallback(event.target.value)}
              autoComplete="off"
              spellCheck={false}
              placeholder="http://127.0.0.1:1455/auth/callback?…"
              disabled={busy || binding.submitted}
            />
            <div className={styles.actions}>
              <Button
                size="sm"
                type="submit"
                disabled={disabled || busy || binding.submitted || !callback.trim()}
              >
                {t('siwc.submit')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={binding.phase === 'cancelling'}
                onClick={() => {
                  setCallback('');
                  void binding.cancel();
                }}
              >
                {t('siwc.cancel')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={busy}
                onClick={() => void binding.check()}
              >
                {t('siwc.check')}
              </Button>
            </div>
          </form>
        </div>
      )}
      {notice && session && (
        <p className={styles.summary} role="status">
          {t(`siwc.${notice}`)}
        </p>
      )}
      {binding.phase === 'loading' && <p role="status">{t('siwc.loading')}</p>}
      {binding.phase === 'ok' && (
        <p className={styles.success} role="status">
          {t('siwc.success')}
        </p>
      )}
      {binding.phase === 'cancelled' && <p role="status">{t('siwc.cancelled')}</p>}
      {binding.error && (
        <p className={styles.error} role="alert">
          {t(`siwc.${binding.error}`)}
        </p>
      )}
    </section>
  );
}
