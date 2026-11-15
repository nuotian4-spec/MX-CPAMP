import type { ApiClientRequestScope } from '@/services/api/client';
import {
  kiloProxiesApi,
  kiloProxyDuplicate,
  kiloAdmissionReason,
  type KiloProxyState,
} from '@/services/api/kiloProxies';

export interface KiloProxyImportRow {
  line: number;
  display: string;
  status:
    | 'valid'
    | 'previewing'
    | 'ready'
    | 'review'
    | 'duplicate'
    | 'error'
    | 'pending'
    | 'saving'
    | 'saved'
    | 'saved_pending'
    | 'rejected'
    | 'unknown';
  observedIP?: string;
  observedAt?: string | null;
  registeredProxyID?: string;
  message: string;
  id?: string;
}
export interface KiloProxyImportPreview {
  rows: KiloProxyImportRow[];
  inputs: Map<number, string>;
}
export function parseKiloProxyLines(raw: string): KiloProxyImportPreview {
  const rows: KiloProxyImportRow[] = [];
  const inputs = new Map<number, string>();
  const seen = new Set<string>();
  raw.split(/\r?\n/).forEach((text, index) => {
    const value = text.trim();
    if (!value) return;
    const line = index + 1;
    try {
      const url = new URL(value);
      if (
        !['http:', 'https:', 'socks5:', 'socks5h:'].includes(url.protocol) ||
        !url.hostname ||
        /\s/.test(value) ||
        (url.port && (!/^\d+$/.test(url.port) || Number(url.port) < 1 || Number(url.port) > 65535))
      )
        throw new Error();
      const normalized = url.href;
      const display = `${url.protocol}//${url.username || url.password ? '***@' : ''}${url.host}${url.search || url.hash || (url.pathname && url.pathname !== '/') ? '（附加部分隐藏）' : ''}`;
      const duplicate = seen.has(normalized);
      rows.push({
        line,
        display,
        status: duplicate ? 'duplicate' : 'valid',
        message: duplicate ? '与本批前一行完整URL重复' : '格式有效，公网IP待后续检测',
      });
      if (!duplicate) inputs.set(line, value);
      seen.add(normalized);
    } catch {
      rows.push({
        line,
        display: '格式错误（原文已隐藏）',
        status: 'error',
        message: '请使用完整 http、https、socks5 或 socks5h 代理URL及有效主机、端口。',
      });
    }
  });
  return { rows, inputs };
}

export async function previewKiloProxyRows({
  preview,
  snapshot,
  scope,
  signal,
  current,
  changed,
}: {
  preview: KiloProxyImportPreview;
  snapshot: KiloProxyState;
  scope: ApiClientRequestScope;
  signal: AbortSignal;
  current: () => boolean;
  changed: (line: number, patch: Partial<KiloProxyImportRow>) => void;
}): Promise<boolean> {
  const seen = new Map<string, number>();
  for (const [line, url] of preview.inputs) {
    if (signal.aborted || !current()) {
      preview.inputs.clear();
      return false;
    }
    changed(line, { status: 'previewing', message: '正在只读观测公网IP' });
    try {
      const result = await kiloProxiesApi.preview(url, snapshot, scope, signal);
      if (signal.aborted || !current()) throw new Error();
      const previous = result.observed_exit_ip ? seen.get(result.observed_exit_ip) : undefined;
      const duplicate = result.status === 'duplicate' || previous !== undefined;
      const patch: Partial<KiloProxyImportRow> = {
        status: duplicate ? 'duplicate' : result.status === 'ready' ? 'ready' : 'review',
        observedIP: result.observed_exit_ip ?? undefined,
        observedAt: result.observed_at,
        registeredProxyID: result.registered_proxy_id,
        message: duplicate
          ? previous !== undefined
            ? `与本批第 ${previous} 行实测公网IP重复`
            : '公网IP已登记，跳过新增'
          : result.status === 'ready'
            ? '可提交；正式保存时重新观测并核验'
            : `待核：${kiloAdmissionReason[result.reason]}；确认后仅保存，不入池`,
      };
      Object.assign(preview.rows.find((row) => row.line === line)!, patch);
      changed(line, patch);
      if (duplicate) preview.inputs.delete(line);
      else if (result.observed_exit_ip) seen.set(result.observed_exit_ip, line);
    } catch {
      changed(line, { status: 'unknown', message: '预览未确认，后续观测已停止；请重新读取状态。' });
      preview.inputs.clear();
      return false;
    }
  }
  return true;
}

export async function importKiloProxyRows({
  inputs,
  snapshot,
  scope,
  signal,
  current,
  changed,
  saved,
}: {
  inputs: Map<number, string>;
  snapshot: KiloProxyState;
  scope: ApiClientRequestScope;
  signal: AbortSignal;
  current: () => boolean;
  changed: (line: number, patch: Partial<KiloProxyImportRow>) => void;
  saved: (snapshot: KiloProxyState) => void;
}): Promise<{ snapshot: KiloProxyState; uncertain: boolean }> {
  let state = snapshot;
  let uncertain = false;
  try {
    for (const [line, url] of inputs) {
      if (signal.aborted || !current()) break;
      changed(line, { status: 'saving', message: '正在保存' });
      inputs.delete(line);
      try {
        const next = await kiloProxiesApi.create({ url, enabled: true }, state, scope, signal);
        if (
          signal.aborted ||
          !current() ||
          next.connection_ref !== snapshot.connection_ref ||
          !Number.isSafeInteger(next.revision) ||
          next.revision <= state.revision
        )
          throw new Error();
        const added = next.proxies.filter(
          (proxy) => !state.proxies.some((before) => before.id === proxy.id)
        );
        if (added.length !== 1) throw new Error();
        state = next;
        const pending = added[0].admission_status === 'pending';
        changed(line, {
          status: pending ? 'saved_pending' : 'saved',
          id: added[0].id,
          observedIP: added[0].observed_exit_ip ?? undefined,
          message: pending
            ? `已保存但未入池：${kiloAdmissionReason[added[0].admission_reason ?? 'unverified']}`
            : added[0].admission_status === 'admitted'
              ? '已保存并登记公网IP；分配仍按既有条件判断'
              : '已保存 · 旧版兼容，准入状态未确认',
        });
        saved(next);
      } catch (error) {
        const duplicate = !signal.aborted && current() ? kiloProxyDuplicate(error) : null;
        if (duplicate) {
          changed(line, {
            status: 'rejected',
            message: '公网IP已登记，本行被拒绝且未保存',
            observedIP: duplicate.ip,
            registeredProxyID: duplicate.proxy_id,
          });
          continue;
        }
        uncertain = true;
        changed(line, {
          status: 'unknown',
          message: '结果待核，后续行已停止；请先读取已保存状态。',
        });
        break;
      }
    }
  } finally {
    inputs.clear();
  }
  if (uncertain && current()) {
    try {
      const read = await kiloProxiesApi.list(scope);
      if (
        current() &&
        read.connection_ref === snapshot.connection_ref &&
        Number.isSafeInteger(read.revision)
      )
        state = read;
    } catch {
      /* 保留部分成功快照，等待显式回读。 */
    }
  }
  return { snapshot: state, uncertain };
}
