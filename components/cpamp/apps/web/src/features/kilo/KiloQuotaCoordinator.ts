import { kiloQuotaApi, type KiloQuotaView } from '@/services/api/kiloQuota';
import type { ApiClientRequestScope } from '@/services/api/client';

type Result = { view: KiloQuotaView; cancelled: boolean };
export type QuotaOutcome = {
  status: 'pending' | 'success' | 'failed' | 'cancelled' | 'skipped';
  reason: string;
};
type Job = {
  id: string;
  revision: number;
  epoch: number;
  controller: AbortController;
  resolve: (result: Result) => void;
  reject: (error: unknown) => void;
  promise: Promise<Result>;
};
export type QuotaPageState = {
  enabled: boolean;
  running: boolean;
  authenticated: boolean;
  views: Record<string, KiloQuotaView>;
  outcomes: Record<string, QuotaOutcome>;
};
const invalidSources = new Set([
  'none',
  'source_missing',
  'source_changed',
  'identity_unknown',
  'no_outer_material',
  'needs_reimport',
  'needs_sign_in',
  'unavailable',
]);

export class KiloQuotaCoordinator {
  private state: QuotaPageState = {
    enabled: false,
    running: false,
    authenticated: true,
    views: {},
    outcomes: {},
  };
  private listeners = new Set<() => void>();
  private ids = new Set<string>();
  private epochs = new Map<string, number>();
  private invalidated = new Set<string>();
  private pending = new Map<string, Job>();
  private roundJobs: Map<string, Job> | undefined;
  private queue: Job[] = [];
  private active = 0;
  private generation = 0;
  private round = 0;
  private readers = new Set<AbortController>();
  private timer: ReturnType<typeof setInterval> | undefined;
  constructor(
    private scope: ApiClientRequestScope,
    private reference: string,
    private onQueryObserved?: (view: KiloQuotaView, signal: AbortSignal) => Promise<void>
  ) {}
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  snapshot = () => this.state;
  private publish(next: Partial<QuotaPageState>) {
    this.state = { ...this.state, ...next };
    this.listeners.forEach((fn) => fn());
  }
  setAccounts(ids: string[]) {
    const next = new Set(ids);
    for (const id of this.ids) if (!next.has(id)) this.invalidate(id, true);
    this.ids = next;
  }
  observe(view: KiloQuotaView) {
    if (!this.ids.has(view.account_id) || view.connection_ref !== this.reference)
      throw { status: 409 };
    const old = this.state.views[view.account_id];
    if (old && view.binding_revision < old.binding_revision) throw { status: 409 };
    if (old && old.binding_revision !== view.binding_revision) this.invalidate(view.account_id);
    this.invalidated.delete(view.account_id);
    this.publish({ views: { ...this.state.views, [view.account_id]: view } });
  }
  invalidate(id: string, removed = false) {
    this.epochs.set(id, (this.epochs.get(id) ?? 0) + 1);
    this.invalidated.add(id);
    for (const job of this.pending.values()) if (job.id === id) job.controller.abort();
    const views = { ...this.state.views };
    if (removed) delete views[id];
    this.publish({
      views,
      outcomes: {
        ...this.state.outcomes,
        [id]: { status: 'skipped', reason: removed ? '账号已删除' : '来源绑定已变化' },
      },
    });
  }
  private outcome(id: string, status: QuotaOutcome['status'], reason: string) {
    this.publish({ outcomes: { ...this.state.outcomes, [id]: { status, reason } } });
  }
  private key(id: string, revision: number) {
    return JSON.stringify([id, revision]);
  }
  query(view: KiloQuotaView, signal?: AbortSignal): Promise<Result> {
    if (
      !this.state.authenticated ||
      !this.ids.has(view.account_id) ||
      this.invalidated.has(view.account_id) ||
      view.connection_ref !== this.reference
    )
      return Promise.reject({ status: 409 });
    const id = view.account_id,
      revision = view.binding_revision;
    if (this.state.views[id]?.binding_revision !== revision) return Promise.reject({ status: 409 });
    const key = this.key(id, revision),
      existing = this.pending.get(key);
    if (existing) {
      signal?.addEventListener('abort', () => existing.controller.abort(), { once: true });
      return existing.promise;
    }
    const controller = new AbortController();
    let resolve!: Job['resolve'], reject!: Job['reject'];
    const promise = new Promise<Result>((yes, no) => {
      resolve = yes;
      reject = no;
    });
    const job: Job = {
      id,
      revision,
      epoch: this.epochs.get(id) ?? 0,
      controller,
      resolve,
      reject,
      promise,
    };
    signal?.addEventListener('abort', () => controller.abort(), { once: true });
    if (signal?.aborted) controller.abort();
    this.pending.set(key, job);
    this.roundJobs?.set(id, job);
    this.queue.push(job);
    this.outcome(id, 'pending', '等待查询');
    this.pump();
    return promise;
  }
  private pump() {
    while (this.active < 3 && this.queue.length) {
      const job = this.queue.shift()!;
      if (
        job.controller.signal.aborted ||
        !this.ids.has(job.id) ||
        (this.epochs.get(job.id) ?? 0) !== job.epoch ||
        this.state.views[job.id]?.binding_revision !== job.revision
      ) {
        this.pending.delete(this.key(job.id, job.revision));
        if (this.state.outcomes[job.id]?.status === 'pending')
          this.outcome(job.id, 'cancelled', '排队请求已取消');
        job.reject({ status: 499 });
        continue;
      }
      this.active += 1;
      void this.execute(job);
    }
  }
  private async execute(job: Job) {
    const version = this.generation;
    try {
      const result = await kiloQuotaApi.query(
        job.id,
        job.revision,
        this.reference,
        this.scope,
        job.controller.signal
      );
      if (
        version !== this.generation ||
        (job.controller.signal.aborted && !result.cancelled) ||
        !this.ids.has(job.id) ||
        (this.epochs.get(job.id) ?? 0) !== job.epoch ||
        this.state.views[job.id]?.binding_revision !== job.revision ||
        result.view.account_id !== job.id ||
        result.view.connection_ref !== this.reference ||
        result.view.binding_revision !== job.revision
      )
        throw { status: 499 };
      this.observe(result.view);
      let policyPending = false;
      try {
        await this.onQueryObserved?.(result.view, job.controller.signal);
      } catch (failure) {
        policyPending = true;
        if ((failure as { status?: number })?.status === 401) this.unauthorized();
      }
      if (
        version !== this.generation ||
        !this.ids.has(job.id) ||
        (this.epochs.get(job.id) ?? 0) !== job.epoch
      )
        throw { status: 499 };
      this.outcome(
        job.id,
        result.cancelled || result.view.query.status === 'cancelled'
          ? 'cancelled'
          : result.view.query.status === 'success'
            ? 'success'
            : 'failed',
        result.view.query.status === 'success'
          ? policyPending
            ? '额度查询成功，来源策略待刷新'
            : '查询成功'
          : result.view.query.status === 'failed'
            ? '查询失败，保留旧观测'
            : '未产生新成功观测'
      );
      job.resolve(result);
    } catch (error) {
      if ((error as { status?: number })?.status === 401) this.unauthorized();
      if (
        version === this.generation &&
        this.ids.has(job.id) &&
        (this.epochs.get(job.id) ?? 0) === job.epoch
      )
        this.outcome(
          job.id,
          job.controller.signal.aborted || (error as { status?: number })?.status === 499
            ? 'cancelled'
            : 'failed',
          '查询结果待核，请读取当前观测'
        );
      job.reject(error);
    } finally {
      this.pending.delete(this.key(job.id, job.revision));
      this.active -= 1;
      this.pump();
    }
  }
  runRound = async () => {
    if (this.state.running || !this.state.authenticated) return;
    const round = ++this.round,
      version = this.generation,
      ids = [...this.ids];
    this.publish({
      running: true,
      outcomes: Object.fromEntries(
        ids.map((id) => [id, { status: 'pending', reason: '读取来源快照' }])
      ),
    });
    const shared = new Map([...this.pending.values()].map((job) => [job.id, job]));
    this.roundJobs = shared;
    const snapshots: { view: KiloQuotaView; epoch: number }[] = [];
    let next = 0;
    await Promise.all(
      Array.from({ length: Math.min(3, ids.length) }, async () => {
        while (next < ids.length && version === this.generation) {
          const id = ids[next++],
            epoch = this.epochs.get(id) ?? 0,
            controller = new AbortController();
          if (this.invalidated.has(id)) {
            this.outcome(id, 'skipped', '来源修改结果待核，请读取当前观测');
            continue;
          }
          const previous = this.state.views[id];
          this.readers.add(controller);
          try {
            let view = await kiloQuotaApi.get(id, this.scope, controller.signal);
            if (version !== this.generation || controller.signal.aborted) return;
            if (!this.ids.has(id) || (this.epochs.get(id) ?? 0) !== epoch) {
              this.outcome(id, 'skipped', '账号或来源已变化');
              continue;
            }
            const latest = this.state.views[id];
            if (latest && latest !== previous && latest.binding_revision === view.binding_revision)
              view = latest;
            this.observe(view);
            if (view.source.kind === 'none' || invalidSources.has(view.source.status))
              this.outcome(id, 'skipped', '无来源或来源已失效');
            else snapshots.push({ view, epoch: this.epochs.get(id) ?? 0 });
          } catch (error) {
            if ((error as { status?: number })?.status === 401) this.unauthorized();
            if (version === this.generation) this.outcome(id, 'failed', '来源快照读取失败');
          } finally {
            this.readers.delete(controller);
          }
        }
      })
    );
    if (version !== this.generation) return;
    await Promise.all(
      snapshots.map(async ({ view, epoch }) => {
        try {
          if (
            !this.ids.has(view.account_id) ||
            this.invalidated.has(view.account_id) ||
            (this.epochs.get(view.account_id) ?? 0) !== epoch
          ) {
            this.outcome(view.account_id, 'skipped', '账号或来源已变化');
            return;
          }
          const existing = shared.get(view.account_id);
          if (existing && existing.revision === view.binding_revision && existing.epoch === epoch)
            await existing.promise;
          else await this.query(view);
        } catch (error) {
          if (
            version === this.generation &&
            this.state.outcomes[view.account_id]?.status === 'pending'
          ) {
            this.outcome(
              view.account_id,
              (error as { status?: number })?.status === 499 ? 'cancelled' : 'failed',
              '本项查询结果待核'
            );
          }
        }
      })
    );
    if (version === this.generation && round === this.round) {
      this.roundJobs = undefined;
      this.publish({ running: false });
    }
  };
  setEnabled(enabled: boolean) {
    if (!enabled) {
      this.stop();
      return;
    }
    if (this.state.enabled || !this.state.authenticated) return;
    this.publish({ enabled: true });
    void this.runRound();
    this.timer = setInterval(
      () => {
        void this.runRound();
      },
      30 * 60 * 1000
    );
  }
  stop() {
    this.generation += 1;
    this.roundJobs = undefined;
    clearInterval(this.timer);
    this.timer = undefined;
    for (const reader of this.readers) reader.abort();
    for (const job of this.pending.values()) job.controller.abort();
    for (const job of this.queue.splice(0)) {
      this.pending.delete(this.key(job.id, job.revision));
      job.reject({ status: 499 });
    }
    const outcomes = Object.fromEntries(
      Object.entries(this.state.outcomes).map(([id, item]) => [
        id,
        item.status === 'pending'
          ? { status: 'cancelled' as const, reason: '本页请求已中止，服务端结果待核' }
          : item,
      ])
    );
    this.publish({ enabled: false, running: false, outcomes });
    this.pump();
  }
  unauthorized() {
    this.stop();
    this.publish({ authenticated: false });
  }
  dispose() {
    this.stop();
    this.listeners.clear();
  }
}
