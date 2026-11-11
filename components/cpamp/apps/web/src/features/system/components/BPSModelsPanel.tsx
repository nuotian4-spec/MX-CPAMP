import { useEffect, useMemo, useRef, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { loadBpsBoundAuthFiles } from '@/services/api/bpsCredentialSnapshot';
import { bpsAccessApi, bpsAccessMessage, type BPSAccess } from '@/services/api/bpsAccess';
import {
  bpsModelsApi,
  describeBpsModelPermission,
  normalizeBpsModelDraft,
  type BPSModelsSnapshot,
} from '@/services/api/bpsModels';
import type { ApiClientRequestScope } from '@/services/api/client';

type Credential = { index: string; name: string; connectionRef: string };

export function BPSModelsPanel({ scope }: { scope: ApiClientRequestScope }) {
  const [snapshot, setSnapshot] = useState<BPSModelsSnapshot>();
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [selected, setSelected] = useState('');
  const [observations, setObservations] = useState<Record<string, BPSAccess>>({});
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [observationsStale, setObservationsStale] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    const current = ++generation.current;
    setBusy(false);
    setSnapshot(undefined);
    setCredentials([]);
    setObservations({});
    setDraft('');
    setSelected('');
    setMessage('');
    Promise.all([bpsModelsApi.get(scope), loadBpsBoundAuthFiles(scope)])
      .then(([config, files]) => {
        if (generation.current !== current) return;
        if (files.files.some((file) => file.bps_connection_ref !== config.connection_ref)) {
          setMessage('CPA连接已变更，请局部刷新');
          return;
        }
        setSnapshot(config);
        setDraft(config.models.join('\n'));
        const next = files.files
          .filter((file) => String(file.type ?? file.provider).toLowerCase() === 'bps')
          .map((file) => ({
            index: String(file.authIndex ?? file.auth_index ?? ''),
            name: file.name,
            connectionRef: String(file.bps_connection_ref ?? ''),
          }))
          .filter((file) => file.index);
        setCredentials(next);
        setSelected(next[0]?.index ?? '');
      })
      .catch(() => {
        if (generation.current === current) setMessage('读取BPS模型设置失败，请刷新页面');
      });
    return () => {
      generation.current = current + 1;
    };
  }, [scope]);
  const observation = observations[selected];
  const discover = async () => {
    const current = generation.current;
    const index = selected;
    setBusy(true);
    setMessage('');
    try {
      const result = await bpsAccessApi.query(
        index,
        scope,
        credentials.find((credential) => credential.index === index)?.connectionRef ?? ''
      );
      if (generation.current !== current) return;
      setObservations((previous) => ({ ...previous, [index]: result }));
      setObservationsStale(false);
      setMessage(bpsAccessMessage(result));
    } catch {
      if (generation.current === current) {
        setObservations((previous) => {
          const next = { ...previous };
          delete next[index];
          return next;
        });
        setMessage('本次权限查询未确认，模型草稿与发布清单保留');
      }
    } finally {
      if (generation.current === current) setBusy(false);
    }
  };
  const refresh = async () => {
    const current = generation.current;
    setBusy(true);
    try {
      const [next, files] = await Promise.all([
        bpsModelsApi.get(scope),
        loadBpsBoundAuthFiles(scope),
      ]);
      if (current === generation.current) {
        if (files.files.some((file) => file.bps_connection_ref !== next.connection_ref))
          throw new Error('connection_changed');
        setSnapshot(next);
        if (credentials[0]?.connectionRef !== next.connection_ref) setObservations({});
        const nextCredentials = files.files
          .filter((file) => String(file.type ?? file.provider).toLowerCase() === 'bps')
          .map((file) => ({
            index: String(file.authIndex ?? file.auth_index ?? ''),
            name: file.name,
            connectionRef: String(file.bps_connection_ref ?? ''),
          }))
          .filter((file) => file.index);
        setCredentials(nextCredentials);
        setSelected((previous) =>
          nextCredentials.some((credential) => credential.index === previous)
            ? previous
            : (nextCredentials[0]?.index ?? '')
        );
        setMessage('发布清单与版本已刷新，编辑草稿保留');
      }
    } catch {
      if (current === generation.current) {
        setSnapshot(undefined);
        setObservationsStale(true);
        setMessage('局部刷新失败，草稿保留，保存暂不可用');
      }
    } finally {
      if (current === generation.current) setBusy(false);
    }
  };
  const save = async () => {
    if (!snapshot) return;
    const current = generation.current;
    setBusy(true);
    setMessage('');
    try {
      await bpsModelsApi.save(snapshot, normalizeBpsModelDraft(draft), scope);
      const next = await bpsModelsApi.get(scope);
      if (next.connection_ref !== snapshot.connection_ref) throw new Error('connection_changed');
      if (generation.current !== current) return;
      setSnapshot(next);
      setDraft(next.models.join('\n'));
      setMessage('BPS全局模型清单已保存，现有逐凭证禁用规则保留');
    } catch {
      if (generation.current === current) {
        setSnapshot(undefined);
        setMessage('保存回执未确认或配置已变更，草稿保留；请局部刷新核对后保存');
      }
    } finally {
      if (generation.current === current) setBusy(false);
    }
  };
  return (
    <Card title="BPS 模型设置">
      <p>全局发布清单作用于当前 CPA 的全部 BPS 凭证。官方允许、本站注册和推理成功分别判断。</p>
      <label>
        发现凭证{' '}
        <select
          aria-label="BPS发现凭证"
          value={selected}
          disabled={busy}
          onChange={(event) => {
            setSelected(event.target.value);
            setMessage('');
          }}
        >
          {!credentials.length && <option value="">暂无BPS凭证</option>}
          {credentials.map((credential) => (
            <option key={credential.index} value={credential.index}>
              {credential.name}
            </option>
          ))}
        </select>
      </label>
      <Button variant="secondary" onClick={() => void discover()} disabled={!selected || busy}>
        获取官方允许模型
      </Button>
      <p>
        全局已发布：{snapshot?.models.length ?? '未确认'}；本凭证官方允许：
        {observation?.models?.length ?? '未确认'}；额度未知，推理未验证。
      </p>
      {observation && (
        <p>
          查询时间：{observation.queried_at}；{observationsStale ? '未更新；' : ''}
          {bpsAccessMessage(observation)}
        </p>
      )}
      {observation?.models && (
        <ul>
          {observation.models.map((id) => (
            <li key={id}>{id}</li>
          ))}
        </ul>
      )}
      <Button
        variant="secondary"
        disabled={busy || !observation || observation.error !== null || observation.models === null}
        onClick={() => setDraft((observation?.models ?? []).join('\n'))}
      >
        一键选用到草稿
      </Button>
      <label style={{ display: 'grid', gap: 8, marginTop: 16 }}>
        全局发布模型草稿（每行一个模型ID）
        <textarea
          aria-label="BPS全局模型草稿"
          rows={7}
          value={draft}
          disabled={busy || !snapshot}
          onChange={(event) => setDraft(event.target.value)}
        />
      </label>
      <p>
        一键选用仅填写草稿。点击保存后生效；保存空清单会移除全局BPS模型目录，凭证与禁用规则保留。
      </p>
      <Button variant="secondary" disabled={busy} onClick={() => void refresh()}>
        局部刷新发布清单
      </Button>
      <Button disabled={busy || !snapshot} onClick={() => void save()}>
        保存 BPS 模型清单
      </Button>
      {message && <p role="status">{message}</p>}
    </Card>
  );
}

export function BPSAccountModelsSummary({
  scope,
  authIndex,
  connectionRef,
  registeredModels,
}: {
  scope: ApiClientRequestScope;
  authIndex: string;
  connectionRef: string;
  registeredModels: string[];
}) {
  const [snapshot, setSnapshot] = useState<BPSModelsSnapshot>();
  const [observation, setObservation] = useState<BPSAccess>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const generation = useRef(0);
  useEffect(() => {
    const current = ++generation.current;
    setSnapshot(undefined);
    setObservation(undefined);
    setMessage('');
    setBusy(false);
    bpsModelsApi
      .get(scope)
      .then((result) => {
        if (current === generation.current) {
          if (connectionRef && result.connection_ref === connectionRef) setSnapshot(result);
          else setMessage('凭证列表的连接引用已过期，请刷新凭证列表');
        }
      })
      .catch(() => {
        if (current === generation.current) setMessage('全局发布清单未确认');
      });
    return () => {
      generation.current = current + 1;
    };
  }, [scope, authIndex, connectionRef]);
  const query = async () => {
    const current = generation.current;
    setBusy(true);
    setMessage('');
    try {
      const result = await bpsAccessApi.query(authIndex, scope, connectionRef);
      if (current === generation.current) {
        setObservation(result);
        setMessage(bpsAccessMessage(result));
      }
    } catch {
      if (current === generation.current) {
        setObservation(undefined);
        setMessage('本次权限查询未确认');
      }
    } finally {
      if (current === generation.current) setBusy(false);
    }
  };
  const models = useMemo(
    () => [
      ...new Set([
        ...(snapshot?.models ?? []),
        ...registeredModels,
        ...(observation?.models ?? []),
      ]),
    ],
    [snapshot, registeredModels, observation]
  );
  return (
    <section aria-label="BPS模型权限与发布">
      <p>
        全局已发布：{snapshot?.models.length ?? '未确认'}；本凭证官方允许：
        {observation?.models?.length ?? '未确认'}；本凭证已注册：{registeredModels.length}。
      </p>
      <p>注册仅表示本站目录，不代表官方权限或真实推理成功。额度未知；推理未验证。</p>
      {snapshot?.models.length === 0 && <p>已登录，尚未发布BPS模型。</p>}
      {snapshot && snapshot.models.length > 0 && registeredModels.length === 0 && (
        <p>已发布模型未注册或被现有禁用规则过滤，请核对下方规则。</p>
      )}
      <a href="#/system">前往系统设置中的 BPS 模型设置</a>
      <Button
        variant="secondary"
        size="sm"
        disabled={busy || !authIndex || !connectionRef || !snapshot}
        onClick={() => void query()}
      >
        查询本凭证官方权限
      </Button>
      {observation && <p>查询时间：{observation.queried_at}</p>}
      {message && <p role="status">{message}</p>}
      <ul>
        {models.map((id) => (
          <li key={id}>
            {id}：
            {describeBpsModelPermission(
              id,
              observation?.models ?? null,
              observation?.allowed ?? null
            )}
            ；{registeredModels.includes(id) ? '本站已注册' : '本站未注册'}
          </li>
        ))}
      </ul>
    </section>
  );
}
