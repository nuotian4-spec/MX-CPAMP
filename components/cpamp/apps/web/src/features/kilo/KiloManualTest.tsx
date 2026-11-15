import { Select } from '@/components/ui/Select';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { KiloAccount } from '@/services/api/kilo';
import { kiloManualApi, kiloManualError, type KiloManualResult } from '@/services/api/kiloManual';
import styles from './KiloAccountsPage.module.scss';
import manualStyles from './KiloManualTest.module.scss';

const statusLabel = {
  completed: '测试完成',
  incomplete: '回答未完成',
  failed: '测试失败',
  canceled: '测试已取消',
};

export function KiloManualTest({
  account,
  scope,
  connectionRef,
  onClose,
}: {
  account: KiloAccount;
  scope: ApiClientRequestScope;
  connectionRef: string;
  onClose: () => void;
}) {
  const [models, setModels] = useState<string[]>([]);
  const [model, setModel] = useState('');
  const [prompt, setPrompt] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const [result, setResult] = useState<KiloManualResult>();
  const generation = useRef(0);
  const request = useRef<AbortController | null>(null);
  const live = useRef({ id: account.id, scope, connectionRef });
  live.current = { id: account.id, scope, connectionRef };
  useEffect(() => {
    const version = ++generation.current;
    const catalog = new AbortController();
    setModels([]);
    setModel('');
    setPrompt('');
    setResult(undefined);
    setLoading(true);
    setBusy(false);
    setError('');
    setNote('');
    const current = () => version === generation.current && !catalog.signal.aborted;
    void kiloManualApi
      .models(scope, catalog.signal)
      .then((next) => {
        if (!current()) return;
        if (
          next.connection_ref !== connectionRef ||
          !Array.isArray(next.models) ||
          next.models.length === 0
        )
          throw { status: 409 };
        setModels(next.models.map((item) => item.id));
      })
      .catch((cause) => {
        if (current()) setError(kiloManualError(cause));
      })
      .finally(() => {
        if (current()) setLoading(false);
      });
    const invalidate = () => {
      generation.current++;
      catalog.abort();
      request.current?.abort();
      request.current = null;
    };
    const unauthorized = () => {
      invalidate();
      setModels([]);
      setResult(undefined);
      setPrompt('');
      setBusy(false);
      setLoading(false);
      setError(kiloManualError({ status: 401 }));
    };
    globalThis.window?.addEventListener('unauthorized', unauthorized);
    return () => {
      invalidate();
      globalThis.window?.removeEventListener('unauthorized', unauthorized);
    };
  }, [account.id, scope, connectionRef]);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (request.current || loading || models.length === 0) return;
    if ((!models.includes(model) && model !== 'kilo-auto/free') || !prompt.trim()) {
      setError('请明确选择测试模型并填写非空文本提示词。');
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    const version = generation.current;
    const current = () =>
      version === generation.current &&
      !controller.signal.aborted &&
      live.current.id === account.id &&
      live.current.scope === scope &&
      live.current.connectionRef === connectionRef;
    setBusy(true);
    setError('');
    setNote('');
    setResult(undefined);
    try {
      const next = await kiloManualApi.test(
        account.id,
        model,
        prompt,
        connectionRef,
        scope,
        controller.signal
      );
      if (!current()) return;
      if (
        next.account_id !== account.id ||
        next.connection_ref !== connectionRef ||
        next.model !== model ||
        !Object.prototype.hasOwnProperty.call(statusLabel, next.status) ||
        typeof next.text !== 'string'
      )
        throw { status: 409 };
      setResult(next);
    } catch (cause) {
      if (current()) setError(kiloManualError(cause));
    } finally {
      if (version === generation.current && request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  };
  const cancel = () => {
    generation.current++;
    request.current?.abort();
    request.current = null;
    setBusy(false);
    setResult(undefined);
    setError('');
    setNote('已中止本页请求，服务端最终结果未确认；未自动重试。');
  };
  return (
    <form className={styles.form} onSubmit={submit} autoComplete="off">
      <p>
        目标账号：<strong>{account.name || account.id}</strong> · {account.id}
      </p>
      <p>当前管理连接：{scope.apiBase}</p>
      <p className={styles.note}>
        这是显式模型生成，不是额度查询或健康检查；模型名含free不承诺免费。账号当前
        {account.enabled ? '已启用' : '人工停用'}
        ，测试不会更改人工启停、解除自动限制或授予永久模型资格。
      </p>
      <label>
        测试模型
        <Select
          ariaLabel="测试模型"
          value={model}
          onChange={setModel}
          disabled={busy || loading || models.length === 0}
          options={[
            { value: '', label: '请选择模型' },
            ...models.map((id) => ({ value: id, label: id })),
            { value: 'kilo-auto/free', label: 'kilo-auto/free（管理专用）' },
          ]}
        />
      </label>
      <p className={styles.note}>目录不是官方实时发现，也不证明当前账号订阅资格。</p>
      <label>
        文本提示词
        <textarea
          aria-label="文本提示词"
          className={manualStyles.prompt}
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          disabled={busy}
          rows={4}
        />
      </label>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {note && <p role="status">{note}</p>}
      {busy && <p role="status">本次测试运行中；仅请求当前账号，不自动切换或重试。</p>}
      {result && (
        <section aria-label="本次手动测试结果">
          <h3>{statusLabel[result.status]}</h3>
          <p>
            模型：{result.model} · 耗时：{result.duration_ms} ms
          </p>
          <p>
            开始：{result.started_at} · 结束：{result.completed_at}
          </p>
          {(result.status === 'completed' || result.status === 'incomplete') && (
            <pre className={manualStyles.output}>{result.text}</pre>
          )}
          {result.status === 'failed' && <p>本次模型请求失败（request_failed）。</p>}
          {result.status === 'canceled' && <p>本次执行已取消（canceled）。</p>}
        </section>
      )}
      <div className={styles.actions}>
        <Button type="submit" disabled={busy || loading || models.length === 0}>
          {busy ? '正在测试' : '开始手动测试'}
        </Button>
        {busy && (
          <Button type="button" variant="secondary" onClick={cancel}>
            取消本次请求
          </Button>
        )}
        <Button type="button" variant="secondary" onClick={onClose}>
          关闭
        </Button>
      </div>
    </form>
  );
}
