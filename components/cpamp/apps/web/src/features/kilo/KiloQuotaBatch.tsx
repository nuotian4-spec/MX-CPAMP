import { useSyncExternalStore } from 'react';
import { Button } from '@/components/ui/Button';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { KiloQuotaCoordinator } from './KiloQuotaCoordinator';
import { KiloQuotaObservation } from './KiloQuotaObservation';
import styles from './KiloAccountsPage.module.scss';
export function KiloQuotaBatch({ coordinator }: { coordinator: KiloQuotaCoordinator }) {
  const state = useSyncExternalStore(coordinator.subscribe, coordinator.snapshot);
  const outcomes = Object.values(state.outcomes);
  const count = (status: string) => outcomes.filter((item) => item.status === status).length;
  return (
    <section className={styles.form} aria-label="批量关联额度">
      <div className={styles.actions}>
        <Button
          disabled={state.running || !state.authenticated}
          onClick={() => void coordinator.runRound()}
        >
          查询已绑定账号额度
        </Button>
        <ToggleSwitch
          label="本页定时查询（30分钟）"
          checked={state.enabled}
          disabled={!state.authenticated}
          onChange={(value) => coordinator.setEnabled(value)}
        />
        {state.running && (
          <Button variant="secondary" onClick={() => coordinator.stop()}>
            停止本页额度查询
          </Button>
        )}
      </div>
      <p role="status">
        {state.running ? '本轮进行中' : '本轮已停止'}：成功 {count('success')} / 失败{' '}
        {count('failed')} / 取消 {count('cancelled')} / 跳过 {count('skipped')} / 等待{' '}
        {count('pending')}
      </p>
      {!state.authenticated && <p role="alert">管理认证已失效，本页查询已停止。</p>}
      <p className={styles.note}>
        定时默认关闭；开启立即查询一轮，之后每30分钟一轮，离页即停止。最多3个查询在途，不自动测试模型。
      </p>
      {Object.entries(state.outcomes).map(([id, item]) => (
        <details key={id}>
          <summary>
            <strong>{id}</strong>：{item.reason}
          </summary>
          {state.views[id] && <KiloQuotaObservation view={state.views[id]} />}
        </details>
      ))}
    </section>
  );
}
