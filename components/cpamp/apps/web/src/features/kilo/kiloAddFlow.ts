import type { ApiClientRequestScope } from '@/services/api/client';
import { kiloApi, type KiloAccountInput } from '@/services/api/kilo';
import type { KiloDeviceSaved } from '@/services/api/kiloDevice';
import {
  kiloProxiesApi,
  type KiloEgressInput,
  type KiloProxyState,
} from '@/services/api/kiloProxies';
import { kiloQuotaApi, type KiloQuotaBinding, type KiloQuotaView } from '@/services/api/kiloQuota';

export interface KiloAddProgress {
  id: string;
  egress: boolean;
  source: boolean;
  done: boolean;
  needsRead: boolean;
  recovered: boolean;
  enabled?: boolean;
  egressWaiting?: boolean;
  error: string;
  stage: 'create' | 'egress' | 'source' | 'enable' | 'done';
}

function sameSet(a?: string[] | null, b?: string[] | null) {
  return a == null || b == null
    ? a == null && b == null
    : a.length === b.length && a.every((x) => b.includes(x));
}

function accountEgress(snapshot: KiloProxyState, id: string) {
  const value = snapshot.accounts.find((account) => account.account_id === id);
  if (!value) return null;
  return {
    mode: value.mode,
    capacityMode: value.capacity_mode ?? 'fixed',
    allocationMode: value.allocation_mode ?? 'auto',
    maxIPs: value.max_ips ?? 1,
    specifiedIPs: [...(value.specified_ips ?? [])].sort(),
    candidates: value.candidate_proxy_ids == null ? null : [...value.candidate_proxy_ids].sort(),
    primary: value.primary_proxy_id || '',
    exclusive: snapshot.proxies
      .filter((proxy) => proxy.account_id === id)
      .map((proxy) => proxy.id)
      .sort(),
  };
}

export class KiloAddFlow {
  progress: KiloAddProgress = {
    id: '',
    egress: false,
    source: false,
    done: false,
    needsRead: false,
    recovered: false,
    error: '',
    stage: 'create',
  };
  private attempted = false;
  private running = false;
  private sourceRevision?: number;
  private egressBefore?: ReturnType<typeof accountEgress>;
  private sourceKind: KiloQuotaBinding['kind'] | 'none';
  private sourceCredential = '';
  private material?: KiloQuotaBinding;
  constructor(
    private scope: ApiClientRequestScope,
    readonly connectionRef: string,
    private egress: KiloEgressInput,
    source: KiloQuotaBinding | undefined,
    private wantsEnabled: boolean,
    private current: () => boolean,
    private changed: (progress: KiloAddProgress) => void
  ) {
    this.material = source;
    this.sourceKind = source?.kind ?? 'none';
    if (source?.kind === 'cpa_codex') this.sourceCredential = source.credential_id;
    this.progress.source = !source;
  }
  clearMaterial() {
    this.material = undefined;
  }
  matchesConnection(scope: ApiClientRequestScope, reference: string) {
    return this.scope === scope && this.connectionRef === reference;
  }
  supplyMaterial(source: KiloQuotaBinding) {
    if (
      source.kind !== this.sourceKind ||
      (source.kind === 'cpa_codex' && source.credential_id !== this.sourceCredential)
    )
      throw new Error('请保留本次已选来源。');
    this.material = source;
  }
  private publish() {
    this.changed({ ...this.progress });
  }
  private check(ref: string) {
    if (!this.current() || ref !== this.connectionRef)
      throw new Error('连接已变化；已知保存结果保留，请返回原连接核实。');
  }
  private fail(error: unknown) {
    this.progress.recovered = true;
    this.progress.needsRead = true;
    this.progress.error =
      error instanceof Error ? error.message : '操作结果待核，请先读取已保存状态。';
    this.clearMaterial();
    this.publish();
  }
  private async guarded(action: () => Promise<void>) {
    if (this.running) return;
    this.running = true;
    try {
      await action();
    } catch (error) {
      this.fail(error);
    } finally {
      this.running = false;
    }
  }
  async create(input: KiloAccountInput) {
    if (this.attempted) return;
    this.attempted = true;
    await this.guarded(async () => {
      this.check(this.connectionRef);
      const account = await kiloApi.create(
        { ...input, enabled: false },
        this.connectionRef,
        this.scope
      );
      this.progress.id = account.id;
      this.progress.enabled = account.enabled;
      this.publish();
      this.check(account.connection_ref);
      if (!account.id || account.enabled) throw new Error('新凭证的停用状态待核，后续写入已停止。');
      await this.finish();
    });
  }
  async deviceSaved(id: string, details?: KiloDeviceSaved) {
    if (this.progress.id) return;
    this.attempted = true;
    this.progress.id = id;
    this.wantsEnabled = false;
    this.publish();
    await this.guarded(async () => {
      this.check(details?.connection_ref ?? this.connectionRef);
      if (
        !details ||
        details.id !== id ||
        details.credential_saved === false ||
        details.egress_error === 'storage_failed' ||
        details.egress_reason === 'storage_failed' ||
        details.egress_reason === 'auto_egress_storage_failed' ||
        details.egress_mode !== this.egress.mode
      ) {
        throw new Error('凭证已保存，正式出口结果待核；请先读取已保存状态。');
      }
      this.progress.enabled = details.enabled;
      this.progress.egress = true;
      this.progress.egressWaiting =
        details.egress_ready === false || details.egress_status === 'pending';
      this.publish();
      await this.finish();
    });
  }
  private validateQuota(view: KiloQuotaView) {
    this.check(view.connection_ref);
    if (view.account_id !== this.progress.id || !Number.isSafeInteger(view.binding_revision))
      throw new Error('额度回执身份待核。');
  }
  private matchesEgress(snapshot: KiloProxyState) {
    const actual = snapshot.accounts.find((a) => a.account_id === this.progress.id);
    if (!actual || actual.mode !== this.egress.mode) return false;
    if (actual.mode === 'direct') return true;
    if (actual.mode === 'auto')
      return (
        (actual.capacity_mode ?? 'fixed') === (this.egress.capacity_mode ?? 'fixed') &&
        (actual.allocation_mode ?? 'auto') === (this.egress.allocation_mode ?? 'auto') &&
        (actual.max_ips ?? 1) === (this.egress.max_ips ?? 1) &&
        sameSet(actual.specified_ips ?? [], this.egress.specified_ips ?? [])
      );
    const exclusive = snapshot.proxies
      .filter((p) => p.account_id === this.progress.id)
      .map((p) => p.id);
    return (
      sameSet(actual.candidate_proxy_ids, this.egress.candidate_proxy_ids) &&
      actual.primary_proxy_id === this.egress.primary_proxy_id &&
      sameSet(exclusive, this.egress.exclusive_proxy_ids)
    );
  }
  private confirmEgress(snapshot: KiloProxyState) {
    if (this.matchesEgress(snapshot)) {
      this.progress.egress = true;
      this.progress.egressWaiting =
        snapshot.accounts.find((account) => account.account_id === this.progress.id)
          ?.egress_ready === false;
      return;
    }
    if (
      this.egressBefore === undefined ||
      JSON.stringify(accountEgress(snapshot, this.progress.id)) !==
        JSON.stringify(this.egressBefore)
    ) {
      throw new Error('出口配置与本次目标或写前状态不同，保留当前配置；请在凭证详情核实。');
    }
  }
  async read() {
    await this.guarded(async () => {
      this.check(this.connectionRef);
      if (!this.progress.id)
        throw new Error('创建回执未确认；请在凭证列表核实，当前页不会再次创建。');
      const accounts = await kiloApi.list(this.scope);
      this.check(accounts.connection_ref);
      const account = accounts.accounts.find((a) => a.id === this.progress.id);
      if (!account) throw new Error('已保存ID未在当前连接找到，请核实原连接。');
      this.progress.enabled = account.enabled;
      if (!this.progress.egress) {
        const proxies = await kiloProxiesApi.list(this.scope);
        this.check(proxies.connection_ref);
        this.confirmEgress(proxies);
      }
      if (!this.progress.source) {
        const view = await kiloQuotaApi.get(this.progress.id, this.scope);
        this.validateQuota(view);
        if (view.source.kind !== 'none') {
          if (
            this.sourceKind === 'cpa_codex' &&
            view.source.kind === 'cpa_codex' &&
            view.source.credential_id === this.sourceCredential &&
            view.binding_revision === (this.sourceRevision ?? -2) + 1 &&
            view.source.status !== 'source_changed'
          )
            this.progress.source = true;
          else throw new Error('已存在额度来源；请在凭证详情核实，当前页保留现有绑定。');
        }
      }
      this.progress.recovered = true;
      this.progress.needsRead = false;
      this.progress.error = '';
      this.publish();
    });
  }
  async resume() {
    if (!this.progress.id || this.progress.needsRead || this.progress.done) return;
    await this.guarded(() => this.finish());
  }
  private async finish() {
    this.check(this.connectionRef);
    const id = this.progress.id;
    if (!this.progress.egress) {
      this.progress.stage = 'egress';
      this.publish();
      const state = await kiloProxiesApi.list(this.scope);
      this.check(state.connection_ref);
      if (this.progress.recovered) this.confirmEgress(state);
      else this.egressBefore = accountEgress(state, id);
      if (!this.progress.egress) {
        const saved = await kiloProxiesApi.egress(id, this.egress, state, this.scope);
        this.check(saved.connection_ref);
        if (!this.matchesEgress(saved)) throw new Error('出口保存回执待核。');
        this.progress.egress = true;
        this.progress.egressWaiting =
          saved.accounts.find((account) => account.account_id === id)?.egress_ready === false;
      }
      this.publish();
    }
    if (!this.progress.source) {
      this.progress.stage = 'source';
      this.publish();
      const before = await kiloQuotaApi.get(id, this.scope);
      this.validateQuota(before);
      if (before.source.kind !== 'none') throw new Error('账号已有来源，请读取核实当前绑定。');
      if (!this.material) throw new Error('请重新填写独立额度材料后继续关联。');
      this.sourceRevision = before.binding_revision;
      const material = this.material;
      this.clearMaterial();
      const after = await kiloQuotaApi.put(
        id,
        material,
        before.binding_revision,
        this.connectionRef,
        this.scope
      );
      this.validateQuota(after);
      if (
        after.binding_revision !== before.binding_revision + 1 ||
        after.source.kind !== this.sourceKind ||
        (this.sourceKind === 'cpa_codex' && after.source.credential_id !== this.sourceCredential)
      )
        throw new Error('额度关联回执待核。');
      this.progress.source = true;
      this.publish();
    }
    if (this.wantsEnabled && !this.progress.recovered) {
      this.progress.stage = 'enable';
      this.publish();
      const snapshot = await kiloApi.list(this.scope);
      this.check(snapshot.connection_ref);
      const account = snapshot.accounts.find((a) => a.id === id);
      if (!account) throw new Error('启用前账号状态待核。');
      this.progress.enabled = account.enabled;
      if (
        !account.enabled &&
        !account.source_policy?.paused &&
        (!account.auto_restriction || account.auto_restriction.reason === 'none')
      ) {
        const saved = await kiloApi.update(id, { enabled: true }, this.connectionRef, this.scope);
        this.check(saved.connection_ref);
        if (saved.id !== id || !saved.enabled) throw new Error('启用结果待核。');
        this.progress.enabled = saved.enabled;
      }
    }
    this.progress.done = true;
    this.progress.stage = 'done';
    this.progress.error = '';
    this.clearMaterial();
    this.publish();
  }
}
