import { useEffect, useRef, useState } from 'react';
import { Drawer } from '@/components/ui/Drawer';
import { Button } from '@/components/ui/Button';
import type { ApiClientRequestScope } from '@/services/api/client';
import { kiloProxiesApi, type KiloProxyState } from '@/services/api/kiloProxies';
import {
  importKiloProxyRows,
  parseKiloProxyLines,
  previewKiloProxyRows,
  type KiloProxyImportPreview,
  type KiloProxyImportRow,
} from './kiloProxyBatch';
import styles from './KiloProxiesPage.module.scss';

const statusText: Record<KiloProxyImportRow['status'], string> = {
  valid: '待预览',
  previewing: '观测中',
  ready: '可接纳',
  review: '待核',
  saved_pending: '已保存待核',
  rejected: '已拒绝',
  duplicate: '批内重复',
  error: '错误',
  pending: '未执行',
  saving: '保存中',
  saved: '已保存',
  unknown: '待核',
};
export function KiloProxyBatchImport({
  scope,
  snapshot,
  blocked,
  acquire,
  release,
  onClose,
  onLocate,
}: {
  scope: ApiClientRequestScope;
  snapshot: KiloProxyState;
  blocked: boolean;
  acquire: () => boolean;
  release: (snapshot: KiloProxyState, uncertain: boolean) => void;
  onClose: () => void;
  onLocate?: (id: string) => void;
}) {
  const [raw, setRaw] = useState('');
  const [rows, setRows] = useState<KiloProxyImportRow[]>([]);
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [started, setStarted] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [note, setNote] = useState('');
  const preview = useRef<KiloProxyImportPreview | undefined>(undefined);
  const inFlight = useRef(false);
  const submitted = useRef(false);
  const alive = useRef(true);
  const abort = useRef<AbortController | undefined>(undefined);
  const connection = useRef({ scope, reference: snapshot.connection_ref });
  const live = useRef({ scope, reference: snapshot.connection_ref });
  live.current = { scope, reference: snapshot.connection_ref };
  const current = () =>
    alive.current &&
    live.current.scope === connection.current.scope &&
    live.current.reference === connection.current.reference;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      abort.current?.abort();
      preview.current?.inputs.clear();
    };
  }, []);
  useEffect(() => {
    setRaw('');
    setRows([]);
    setConsent(false);
    preview.current?.inputs.clear();
    abort.current?.abort();
  }, [scope, snapshot.connection_ref]);
  const close = () => {
    setRaw('');
    preview.current?.inputs.clear();
    onClose();
  };
  const submit = async () => {
    const draft = preview.current;
    if (
      inFlight.current ||
      submitted.current ||
      !consent ||
      !draft?.inputs.size ||
      blocked ||
      !current() ||
      !acquire()
    )
      return;
    submitted.current = true;
    inFlight.current = true;
    setStarted(true);
    setBusy(true);
    setRaw('');
    setNote('');
    setRows(
      draft.rows.map((row) =>
        row.status === 'ready' || row.status === 'review'
          ? { ...row, status: 'pending', message: '尚未执行' }
          : row
      )
    );
    const controller = new AbortController();
    abort.current = controller;
    try {
      const result = await importKiloProxyRows({
        inputs: draft.inputs,
        snapshot,
        scope,
        signal: controller.signal,
        current,
        changed: (line, patch) => {
          if (current())
            setRows((old) => old.map((row) => (row.line === line ? { ...row, ...patch } : row)));
        },
        saved: () => {},
      });
      if (current()) {
        setUncertain(result.uncertain);
        setNote(
          result.uncertain
            ? '部分结果待核。成功行不会重放，待核行不按地址猜测身份。'
            : controller.signal.aborted
              ? '本批已停止，尚未执行行保持未执行。'
              : '本批已处理；已保存待核行尚未入池，明确拒绝行未创建。'
        );
        release(result.snapshot, result.uncertain);
      }
    } finally {
      draft.inputs.clear();
      preview.current = undefined;
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  };
  const read = async () => {
    if (inFlight.current || !current()) return;
    inFlight.current = true;
    setBusy(true);
    const controller = new AbortController();
    abort.current = controller;
    try {
      const state = await kiloProxiesApi.list(scope, controller.signal);
      if (
        !current() ||
        controller.signal.aborted ||
        state.connection_ref !== connection.current.reference
      )
        throw new Error();
      release(state, false);
      setNote(
        '已读取当前出口池。待核行的认证与创建身份仍须人工核实；此页不会重发该行或成功行。未执行行可另开新批次提交。'
      );
    } catch {
      if (alive.current) setNote('读取未完成，请保留结果并返回原连接核实。');
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    }
  };
  const observe = async () => {
    if (inFlight.current || blocked || !current() || !acquire()) return;
    preview.current?.inputs.clear();
    const draft = parseKiloProxyLines(raw);
    preview.current = draft;
    setRows(draft.rows);
    setConsent(false);
    setNote('');
    setBusy(true);
    inFlight.current = true;
    const controller = new AbortController();
    abort.current = controller;
    let confirmed = false;
    let refreshed = false;
    let latest = snapshot;
    try {
      confirmed = await previewKiloProxyRows({
        preview: draft,
        snapshot,
        scope,
        signal: controller.signal,
        current,
        changed: (line, patch) => {
          if (current())
            setRows((old) => old.map((row) => (row.line === line ? { ...row, ...patch } : row)));
        },
      });
      if (current()) {
        if (!confirmed) {
          try {
            const state = await kiloProxiesApi.list(scope);
            if (
              current() &&
              state.connection_ref === connection.current.reference &&
              Number.isSafeInteger(state.revision)
            ) {
              latest = state;
              refreshed = true;
            }
          } catch {
            /* 显式刷新保留。 */
          }
        }
        if (current())
          setNote(
            confirmed
              ? '预览不登记、不占位；正式提交会重新观测。待核行需确认后保存。'
              : refreshed
                ? '预览未确认，已读取当前状态；请重新预览。'
                : '预览未确认，请刷新后重新预览。'
          );
      }
    } finally {
      inFlight.current = false;
      if (alive.current) setBusy(false);
      if (current()) release(latest, !confirmed && !refreshed);
    }
  };
  const valid = rows.filter(
    (row) =>
      (row.status === 'ready' || row.status === 'review') && preview.current?.inputs.has(row.line)
  ).length;
  return (
    <Drawer
      open
      title="批量导入代理"
      width={650}
      onClose={close}
      onBeforeClose={() => {
        if (inFlight.current) {
          abort.current?.abort();
          return false;
        }
        return true;
      }}
      footer={
        <div className={styles.actions}>
          <Button variant="secondary" onClick={() => (busy ? abort.current?.abort() : close())}>
            {busy ? '停止后续导入' : '关闭'}
          </Button>
          {!started && (
            <Button disabled={busy || blocked || !consent || !valid} onClick={() => void submit()}>
              仅导入有效行
            </Button>
          )}
          {started && uncertain && (
            <Button disabled={busy} onClick={() => void read()}>
              读取已保存状态
            </Button>
          )}
        </div>
      }
    >
      <div className={styles.importBody}>
        <p>一行一条完整代理 URL；支持 http、https、socks5、socks5h。空行忽略。</p>
        {!started && (
          <>
            <label className={styles.inputLabel}>
              每行一条代理 URL
              <textarea
                aria-label="每行一条代理 URL"
                value={raw}
                autoComplete="off"
                spellCheck={false}
                disabled={busy}
                placeholder={'http://192.0.2.80:8080\nsocks5://USER:PASS@192.0.2.81:1080'}
                onChange={(event) => {
                  preview.current?.inputs.clear();
                  preview.current = undefined;
                  setRows([]);
                  setConsent(false);
                  setRaw(event.target.value);
                }}
              />
            </label>
            <small>输入仅留在当前内存；提交、关闭或切换连接后清空。预览隐藏认证信息。</small>
            <Button
              variant="secondary"
              disabled={busy || !raw.trim()}
              onClick={() => void observe()}
            >
              逐行预览
            </Button>
          </>
        )}
        {!!rows.length && (
          <>
            <div className={styles.importCounts}>
              <span>可接纳 {rows.filter((row) => row.status === 'ready').length}</span>
              <span>待核 {rows.filter((row) => row.status === 'review').length}</span>
              <span>批内重复 {rows.filter((row) => row.status === 'duplicate').length}</span>
              <span>错误 {rows.filter((row) => row.status === 'error').length}</span>
              {started && (
                <span>
                  已保存{' '}
                  {
                    rows.filter((row) => row.status === 'saved' || row.status === 'saved_pending')
                      .length
                  }
                </span>
              )}
            </div>
            <ol className={styles.importRows} aria-label="代理逐行结果">
              {rows.map((row) => (
                <li key={row.line}>
                  <span>{row.line}</span>
                  <div>
                    <code>{row.display}</code>
                    <small>{row.message}</small>
                    {row.observedIP && <small>实测公网IP：{row.observedIP}</small>}
                    {row.observedAt && (
                      <small>观测：{new Date(row.observedAt).toLocaleString()}</small>
                    )}
                    {row.registeredProxyID && (
                      <small>
                        原记录：{row.registeredProxyID}{' '}
                        <Button
                          variant="secondary"
                          disabled={busy}
                          onClick={() => onLocate?.(row.registeredProxyID!)}
                        >
                          查看原记录
                        </Button>
                      </small>
                    )}
                    {row.id && <small>代理ID：{row.id}</small>}
                  </div>
                  <strong>{statusText[row.status]}</strong>
                </li>
              ))}
            </ol>
            {!started && (
              <label className={styles.consent}>
                <input
                  type="checkbox"
                  checked={consent}
                  onChange={(e) => setConsent(e.target.checked)}
                />
                确认提交 {valid} 条；待核行仅保存不入池，跳过重复与错误行。
              </label>
            )}
          </>
        )}
        {!started && (
          <p>按服务端实测公网IP核重复；预览不代替正式提交门禁，不推断已保存入口认证信息。</p>
        )}
        {note && <p role="status">{note}</p>}
      </div>
    </Drawer>
  );
}
