import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { KiloAccountsPanel } from './KiloAccountsPage';
import { KiloQuotaBatch } from './KiloQuotaBatch';
vi.mock('@/stores', () => ({ useAuthStore: vi.fn() }));
declare const process: { env: Record<string, string | undefined> };
const managerURL = process.env.KILO_BATCH_MANAGER_URL,
  key = process.env.KILO_BATCH_MANAGER_KEY,
  controlURL = process.env.KILO_BATCH_CONTROL_URL;
it.skipIf(!managerURL || !key || !controlURL)(
  '真实页面批量经Manager和Core完成四项额度查询',
  async () => {
    for (const raw of [managerURL!, controlURL!]) {
      const url = new URL(raw);
      expect(url.protocol).toBe('http:');
      expect(['127.0.0.1', '[::1]']).toContain(url.hostname);
    }
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    const fixture = (await (await fetch(controlURL + '/fixture')).json()) as {
      account_ids: string[];
      credential_id: string;
    };
    expect(fixture.account_ids).toHaveLength(4);
    let renderer: ReactTestRenderer | undefined;
    try {
      await act(async () => {
        renderer = create(
          <MemoryRouter>
            <KiloAccountsPanel scope={{ apiBase: managerURL!, managementKey: key! }} />
          </MemoryRouter>
        );
      });
      const until = async (check: () => boolean) => {
        const limit = Date.now() + 30000;
        while (!check()) {
          if (Date.now() > limit) throw Error('真实批量组件等待超时');
          await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 20));
          });
        }
      };
      await until(() => renderer!.root.findAllByType(KiloQuotaBatch).length === 1);
      await act(async () => {
        renderer!.root
          .findAllByType(Button)
          .find((item) => item.props.children === '查询已绑定账号额度')!
          .props.onClick();
      });
      const coordinator = renderer!.root.findByType(KiloQuotaBatch).props.coordinator;
      await until(() => !coordinator.snapshot().running);
      const state = coordinator.snapshot();
      expect(Object.keys(state.outcomes).sort()).toEqual([...fixture.account_ids].sort());
      for (const id of fixture.account_ids) {
        expect(state.outcomes[id].status).toBe('success');
        expect(state.views[id]).toMatchObject({
          account_id: id,
          binding_revision: 1,
          source: { kind: 'cpa_codex', credential_id: fixture.credential_id },
          query: { status: 'success' },
        });
        expect(
          state.views[id].windows.some(
            (window: { remaining_percent: number }) => window.remaining_percent === 75
          )
        ).toBe(true);
      }
      expect(JSON.stringify(renderer!.toJSON())).toContain('75%');
      const verified = await fetch(controlURL + '/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      });
      expect(verified.status).toBe(200);
    } finally {
      await act(async () => renderer?.unmount());
      vi.unstubAllGlobals();
    }
  },
  60000
);
