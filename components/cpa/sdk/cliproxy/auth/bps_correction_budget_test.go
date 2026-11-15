package auth

import (
	"context"
	"testing"
)

func TestBPSParityCorrectionBudget(t *testing.T) {
	s := &bpsRequestState{attempts: 1, independent: true}
	ctx := context.WithValue(context.Background(), bpsRequestKey{}, s)
	if BeginBPSToolCorrection(ctx) != nil || s.independent {
		t.Fatal("correction did not disable failover")
	}
	if BeginBPSToolCorrection(ctx) != nil || BeginBPSToolCorrection(ctx) == nil {
		t.Fatal("correction budget not bounded")
	}
	s2 := &bpsRequestState{attempts: 2, independent: true}
	if BeginBPSToolCorrection(context.WithValue(context.Background(), bpsRequestKey{}, s2)) == nil {
		t.Fatal("failover B admitted correction")
	}
}
