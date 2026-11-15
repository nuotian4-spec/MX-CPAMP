import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloApi,
  kiloOrganizationLabel,
  type KiloAccount,
  type KiloOrganizations,
} from '@/services/api/kilo';
import styles from './KiloAccountsPage.module.scss';

export function KiloOrganizationPicker({
  account,
  scope,
  connectionRef,
  disabled,
  value,
  onChange,
}: {
  account: KiloAccount;
  scope: ApiClientRequestScope;
  connectionRef: string;
  disabled: boolean;
  value: string | undefined;
  onChange: (value: string | undefined) => void;
}) {
  const [snapshot, setSnapshot] = useState<KiloOrganizations>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const live = useRef({ scope, connectionRef, id: account.id });
  live.current = { scope, connectionRef, id: account.id };
  useEffect(() => {
    generation.current += 1;
    setSnapshot(undefined);
    setError('');
    setLoading(false);
    request.current = null;
    return () => {
      generation.current += 1;
      request.current?.abort();
    };
  }, [scope, connectionRef, account.id]);
  const read = async () => {
    if (disabled || request.current) return;
    const version = generation.current,
      id = account.id;
    const current = () =>
      generation.current === version &&
      live.current.scope === scope &&
      live.current.connectionRef === connectionRef &&
      live.current.id === id;
    const abort = new AbortController();
    request.current = abort;
    setLoading(true);
    setError('');
    try {
      const result = await kiloApi.organizations(id, scope, abort.signal);
      if (!current()) return;
      if (abort.signal.aborted) throw { status: 499 };
      if (result.connection_ref !== connectionRef) throw { status: 409 };
      setSnapshot(result);
    } catch (failure) {
      if (!current()) return;
      const status = (failure as { status?: number })?.status;
      setError(
        status === 409
          ? '组织查询期间身份或连接已变化，请关闭编辑并刷新账号列表。'
          : status === 499 || abort.signal.aborted
            ? '组织查询已中止，当前账号和输入保持不变。'
            : status === 403
              ? '当前Key没有组织查询权限；当前账号和输入保持不变。'
              : '组织列表读取失败，当前账号和输入保持不变；请显式重试。'
      );
    } finally {
      if (current()) {
        request.current = null;
        setLoading(false);
      }
    }
  };
  const options = [
    { value: ':keep', label: '保留当前组织，不提交组织字段' },
    ...(snapshot?.has_personal_account ? [{ value: '', label: '个人空间' }] : []),
    ...(snapshot?.organizations ?? []).map((org) => ({
      value: org.id,
      label: `${org.name}（${org.id}）`,
    })),
  ];
  return (
    <section className={styles.form} aria-label="组织选择">
      <p>当前组织：{kiloOrganizationLabel(account.organization_id)}</p>
      <Button
        type="button"
        variant="secondary"
        disabled={disabled || loading}
        onClick={() => void read()}
      >
        {loading ? '正在读取组织' : '读取当前Key可用组织'}
      </Button>
      {loading && (
        <Button type="button" variant="secondary" onClick={() => request.current?.abort()}>
          中止组织查询
        </Button>
      )}
      {snapshot && (
        <>
          <Select
            ariaLabel="保存组织"
            value={value ?? ':keep'}
            options={options}
            disabled={disabled || loading}
            onChange={(next) => {
              if (options.some((option) => option.value === next))
                onChange(next === ':keep' ? undefined : next);
            }}
          />
          {!snapshot.has_personal_account && <p className={styles.note}>当前Key不允许个人空间。</p>}
          {snapshot.organizations.length === 0 && <p>当前Key没有可用组织。</p>}
        </>
      )}
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      <p className={styles.note}>
        列表仅属于当前服务端Key，新输入的API
        Key不用于此查询。显式选择组织或换Key并保留组织时，保存会以待用Key重新复验；换Key后仍可能拒绝。普通编辑默认保留组织，不提交组织字段。查询不是模型验证。
      </p>
    </section>
  );
}
