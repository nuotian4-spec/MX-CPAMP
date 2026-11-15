import { expect, it } from 'vitest';
import type { TFunction } from 'i18next';
import { getPlanPresentation } from './presentation';
const t = ((key: string, options?: { defaultValue?: string }) =>
  options?.defaultValue ?? key) as TFunction;
it.each(['bps', 'cpa-bps'])('注册%s不套用Codex倍率', (provider) => {
  expect(getPlanPresentation({ provider, planType: 'pro', t })?.fullLabel).toBe('Pro');
  expect(
    getPlanPresentation({ provider, planType: 'self_serve_business_prolite', t })?.fullLabel
  ).toBe('self_serve_business_prolite');
  expect(getPlanPresentation({ provider, planType: '', t })).toBeNull();
  const name = 'Enterprise custom long plan '.repeat(20);
  expect(getPlanPresentation({ provider, planType: name, t })?.fullLabel).toBe(name.trim());
});
