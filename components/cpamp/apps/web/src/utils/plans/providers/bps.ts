import { normalizeRawPlanType } from '../normalize';
import type { PlanResolverDescriptor } from './types';
const names: Readonly<Record<string, string>> = {
  free: 'Free',
  plus: 'Plus',
  pro: 'Pro',
  team: 'Team',
  business: 'Business',
  enterprise: 'Enterprise',
  self_serve_business_prolite: 'self_serve_business_prolite',
};
export const resolveBpsPlanDescriptor = (raw: unknown): PlanResolverDescriptor | null => {
  const plan = normalizeRawPlanType(raw);
  if (!plan || !names[plan]) return null;
  return {
    canonicalPlanType: 'bps:' + plan,
    shortLabelKey: 'plans.bps.' + plan,
    shortDefault: names[plan],
  };
};
