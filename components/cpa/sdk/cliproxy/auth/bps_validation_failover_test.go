package auth

import (
	"context"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"testing"
)

type classifiedFailoverFacts struct {
	failoverFacts
	reason, validation string
}

func (f classifiedFailoverFacts) BPSFailoverReason() (string, string) { return f.reason, f.validation }

func TestBPSValidationFailoverClassification(t *testing.T) {
	for _, reason := range []string{"schema_mismatch", "nesting_limit", "number_precision_limit", "number_exponent_limit", "number_budget_limit", "", "PRIVATE_REASON"} {
		t.Run(reason, func(t *testing.T) {
			req := coreexecutor.Request{Model: "m", Payload: []byte(`{"input":"hi","tools":[{}]}`)}
			ctx := WithBPSRequestState(context.Background(), []string{"bps"}, req, coreexecutor.Options{})
			a := &Auth{ID: "A", Provider: "bps", Metadata: map[string]any{"account_id": "A", "sub": "A"}}
			b := &Auth{ID: "B", Provider: "bps", Metadata: map[string]any{"account_id": "B", "sub": "B"}}
			if err := beginBPSDispatch(ctx, a, req, coreexecutor.Options{}); err != nil {
				t.Fatal(err)
			}
			failure := classifiedFailoverFacts{failoverFacts{true, true}, "schema_validation", reason}
			allowed := authorizeBPSFailover(ctx, failure)
			if allowed != (reason == "schema_mismatch") {
				t.Fatalf("allowed=%v reason=%s", allowed, reason)
			}
			dispatches := 1
			if allowed {
				for _, provider := range []string{"codex", "kilo", "cpa-bps"} {
					other := b.Clone()
					other.Provider = provider
					if len(bpsFailoverModels(ctx, other, []string{"m"})) != 0 || beginBPSDispatch(ctx, other, req, coreexecutor.Options{}) == nil {
						t.Fatalf("cross-provider %s admitted", provider)
					}
				}
				if err := beginBPSDispatch(ctx, b, req, coreexecutor.Options{}); err != nil {
					t.Fatal(err)
				}
				dispatches++
				if authorizeBPSFailover(ctx, failure) || beginBPSDispatch(ctx, a, req, coreexecutor.Options{}) == nil {
					t.Fatal("third generation admitted")
				}
			} else if beginBPSDispatch(ctx, b, req, coreexecutor.Options{}) == nil {
				t.Fatal("resource rejection switched account")
			}
			if BPSGenerationCount(ctx) != dispatches {
				t.Fatal("generation budget changed")
			}
			t.Logf("A=1 B=%d cross_provider=0", dispatches-1)
		})
	}
}

func TestBPSSchemaFailoverPreservesDenialConditions(t *testing.T) {
	for _, condition := range []string{"independent", "delivered", "client_delivery", "pending", "exhausted", "not_completed", "not_withheld", "cancelled", "pin", "session", "model", "payload", "wire", "subject"} {
		t.Run(condition, func(t *testing.T) {
			req := coreexecutor.Request{Model: "m", Payload: []byte(`{"input":"hi","tools":[{}]}`)}
			opts := coreexecutor.Options{Metadata: map[string]any{}}
			if condition == "pin" {
				opts.Metadata[coreexecutor.PinnedAuthMetadataKey] = "A"
			}
			if condition == "session" {
				opts.Metadata[coreexecutor.ExecutionSessionMetadataKey] = "session"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, mark := WithBPSClientDelivery(ctx)
			ctx = WithBPSRequestState(ctx, []string{"bps"}, req, opts)
			a := &Auth{ID: "A", Provider: "bps", Metadata: map[string]any{"account_id": "A", "sub": "A"}}
			b := &Auth{ID: "B", Provider: "bps", Metadata: map[string]any{"account_id": "B", "sub": "B"}}
			if err := beginBPSDispatch(ctx, a, req, opts); err != nil {
				t.Fatal(err)
			}
			failure := classifiedFailoverFacts{failoverFacts{true, true}, "schema_validation", "schema_mismatch"}
			state := bpsState(ctx)
			switch condition {
			case "independent":
				state.independent = false
			case "delivered":
				state.delivered = true
			case "client_delivery":
				mark()
			case "pending":
				state.pending = true
			case "exhausted":
				state.attempts = 2
			case "not_completed":
				failure.completed = false
			case "not_withheld":
				failure.withheld = false
			case "cancelled":
				cancel()
			case "subject":
				state.subject = ""
			}
			allowed := authorizeBPSFailover(ctx, failure)
			if condition == "model" || condition == "payload" || condition == "wire" {
				if !allowed {
					t.Fatal("valid initial facts denied")
				}
				switch condition {
				case "model":
					req.Model = "other"
				case "payload":
					req.Payload = []byte(`{"input":"changed","tools":[{}]}`)
				case "wire":
					if err := CheckBPSFailoverPayload(ctx, []byte(`{"input":"hi"}`)); err != nil {
						t.Fatal(err)
					}
				}
				err := beginBPSDispatch(ctx, b, req, opts)
				if condition == "wire" {
					if err != nil {
						t.Fatal(err)
					}
					err = CheckBPSFailoverPayload(ctx, []byte(`{"input":"changed"}`))
				}
				if err == nil {
					t.Fatal("changed model or payload admitted")
				}
			} else if allowed {
				t.Fatal("denial condition admitted")
			}
		})
	}
}
