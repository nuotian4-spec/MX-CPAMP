import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { kiloQuotaSourceLabel, type KiloQuotaSources } from '@/services/api/kiloQuota';
import styles from './KiloAccountsPage.module.scss';

export interface KiloQuotaSourceDraft {
  mode: 'none' | 'cpa_codex' | 'imported';
  selected: string;
  search: string;
  access: string;
  outerID: string;
}
export function KiloQuotaSourceFields({
  value: valueState,
  onChange,
  sources,
  busy,
  loadSources,
  allowLater = false,
  onModeChange,
}: {
  value: KiloQuotaSourceDraft;
  onChange: (value: KiloQuotaSourceDraft) => void;
  sources?: KiloQuotaSources;
  busy: boolean;
  loadSources: () => Promise<void>;
  allowLater?: boolean;
  onModeChange?: () => void;
}) {
  const { mode, selected, search: sourceSearch, access, outerID } = valueState;
  const selectedSource = sources?.sources.find((source) => source.credential_id === selected);
  const visibleSources = sources?.sources.filter((source) =>
    `${source.label} ${source.credential_id}`
      .toLowerCase()
      .includes(sourceSearch.trim().toLowerCase())
  );
  return (
    <>
      <div className={styles.quotaSourceModes} aria-label="额度来源类型">
        {(allowLater
          ? (['cpa_codex', 'imported', 'none'] as const)
          : (['cpa_codex', 'imported'] as const)
        ).map((value) => (
          <button
            key={value}
            type="button"
            data-source-mode={value}
            aria-pressed={mode === value}
            disabled={!!busy}
            onClick={() => {
              onModeChange?.();
              onChange({ ...valueState, mode: value, access: '', selected: '', outerID: '' });
            }}
          >
            {value === 'cpa_codex'
              ? '关联现有Codex'
              : value === 'imported'
                ? '独立access导入'
                : '稍后关联'}
          </button>
        ))}
      </div>
      {mode === 'cpa_codex' ? (
        <>
          <Input
            label="搜索 Codex 凭证"
            value={sourceSearch}
            placeholder="账号名或 AuthID"
            disabled={!!busy}
            onChange={(event) => onChange({ ...valueState, search: event.target.value })}
          />
          <div
            className={styles.quotaSourceCandidates}
            role="radiogroup"
            aria-label="精确Codex来源"
          >
            {(visibleSources ?? []).map((source) => (
              <button
                key={source.credential_id}
                type="button"
                role="radio"
                data-source-id={source.credential_id}
                aria-checked={selected === source.credential_id}
                disabled={!!busy || !source.chatgpt_account_id}
                className={styles.quotaSourceCandidate}
                onClick={() => onChange({ ...valueState, selected: source.credential_id })}
              >
                <span>
                  <strong>{source.label}</strong>
                  <small>套餐未知 · Codex</small>
                </span>
                <span>
                  <small>AuthID {source.credential_id}</small>
                  <small>{kiloQuotaSourceLabel[source.status]}</small>
                </span>
                <small>外部账号 {source.chatgpt_account_id ?? '未知，尚未具备绑定身份'}</small>
                <small>额度快照未知</small>
              </button>
            ))}
            {!sources && (
              <p className={styles.note}>{busy ? '正在读取本地来源列表…' : '本地来源列表待重读'}</p>
            )}
            {sources && !visibleSources?.length && (
              <p className={styles.note}>没有匹配的 Codex 凭证</p>
            )}
          </div>
          <div className={styles.actions}>
            <small className={styles.note}>
              {selectedSource
                ? '已选：' + selectedSource.label
                : '请选择一份身份完整的 Codex 凭证。'}
            </small>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              disabled={!!busy}
              onClick={() => void loadSources()}
            >
              读取可引用来源
            </Button>
          </div>
          <small className={styles.note}>候选套餐与额度快照：未知。选择来源不发起额度查询。</small>
          {selectedSource?.status === 'refresh_required' && (
            <small className={styles.note}>查询时由原生命周期尝试续期。</small>
          )}
        </>
      ) : mode === 'imported' ? (
        <>
          <Input
            label="独立access token（仅写入）"
            type="password"
            autoComplete="new-password"
            value={access}
            disabled={!!busy}
            onChange={(event) => onChange({ ...valueState, access: event.target.value })}
          />
          <Input
            label="outer账号ID（可选）"
            value={outerID}
            disabled={!!busy}
            onChange={(event) => onChange({ ...valueState, outerID: event.target.value })}
          />
          <p className={styles.note}>
            仅接受access token；不导入refresh token、Cookie或密码。不回显旧材料，提交后立即清空。
          </p>
        </>
      ) : (
        <p className={styles.note}>保存后可在凭证详情关联额度来源。</p>
      )}
    </>
  );
}
