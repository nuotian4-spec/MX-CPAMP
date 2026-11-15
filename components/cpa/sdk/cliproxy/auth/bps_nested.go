package auth

import "context"

type bpsNestedContext struct{ context.Context }

func (c bpsNestedContext) Value(key any) any {
	switch key.(type) {
	case bpsRequestKey, bpsDeliveryKey, permissionDispatchKey, roundTripperContextKey,
		concurrencyBudgetKey, requiredAuthKindContextKey, credentialPolicyContextKey, homeAttemptContextKey,
		weightedTransportProviderKey, weightedAccountingPoolKey:
		return nil
	}
	return c.Context.Value(key)
}

// WithoutBPSExecutionState isolates nested search dispatch from the parent account budget.
func WithoutBPSExecutionState(ctx context.Context) context.Context { return bpsNestedContext{ctx} }

// BeginBPSWebSearch prevents account replay after a server-side search has started.
func BeginBPSWebSearch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s := bpsState(ctx); s != nil {
		s.Lock()
		s.independent = false
		s.Unlock()
	}
	return CheckBPSPermissionDispatch(ctx)
}
