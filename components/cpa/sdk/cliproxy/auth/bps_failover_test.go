package auth

import (
	"context"
	"errors"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"testing"
)

type failoverFacts struct{ completed, withheld bool }

func (failoverFacts) Error() string                    { return "synthetic local rejection" }
func (f failoverFacts) BPSFailoverFacts() (bool, bool) { return f.completed, f.withheld }

func TestBPSFailoverRequestBoundary(t *testing.T) {
	valid := `{"input":"hello","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`
	for _, raw := range []string{
		`{"input":[{"type":"compaction","encrypted_content":"opaque"}],"tools":[{}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_file","file_id":"f"}]}],"tools":[{}]}`,
		`{"input":[{"role":"assistant","tool_calls":[{}],"content":"done"}],"tools":[{}]}`,
		`{"input":"hello","previous_response_id":"old","tools":[{}]}`,
		`{"input":[{"role":"tool","content":"done"}],"tools":[{}]}`,
	} {
		if bpsIndependentRequest([]byte(raw)) {
			t.Errorf("account-bound input admitted: %s", raw)
		}
	}
	for _, tc := range []struct {
		name                       string
		facts                      error
		delivered, pinned, session bool
		want                       bool
	}{
		{name: "trusted", facts: failoverFacts{true, true}, want: true},
		{name: "untyped", facts: errors.New("completed no tool executed")},
		{name: "incomplete", facts: failoverFacts{false, true}},
		{name: "dispatched", facts: failoverFacts{true, false}},
		{name: "delivered", facts: failoverFacts{true, true}, delivered: true},
		{name: "pin", facts: failoverFacts{true, true}, pinned: true},
		{name: "session", facts: failoverFacts{true, true}, session: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := coreexecutor.Options{Metadata: map[string]any{}}
			if tc.pinned {
				opts.Metadata[coreexecutor.PinnedAuthMetadataKey] = "A"
			}
			if tc.session {
				opts.Metadata[coreexecutor.ExecutionSessionMetadataKey] = "session"
			}
			req := coreexecutor.Request{Model: "model", Payload: []byte(valid)}
			ctx := WithBPSRequestState(context.Background(), []string{"bps"}, req, opts)
			a := &Auth{ID: "A", Provider: "bps", Metadata: map[string]any{"account_id": "accountA", "sub": "subA"}}
			if e := beginBPSDispatch(ctx, a, req, opts); e != nil {
				t.Fatal(e)
			}
			if tc.delivered {
				MarkBPSClientEvent(ctx)
			}
			if got := authorizeBPSFailover(ctx, tc.facts); got != tc.want {
				t.Fatalf("authorized=%v want=%v", got, tc.want)
			}
			if !tc.want {
				return
			}
			if e := beginBPSDispatch(ctx, a, req, opts); e == nil {
				t.Fatal("same AuthID replayed")
			}
			b := &Auth{ID: "B", Provider: "bps", Metadata: map[string]any{"account_id": "accountB", "sub": "subB"}}
			if e := beginBPSDispatch(ctx, b, req, opts); e != nil {
				t.Fatal(e)
			}
			if authorizeBPSFailover(ctx, tc.facts) {
				t.Fatal("third attempt allowed")
			}
		})
	}
}

func TestBPSFailoverFinalPayloadAndDelivery(t *testing.T) {
	req := coreexecutor.Request{Model: "m", Payload: []byte(`{"input":"hi","tools":[{}]}`)}
	ctx, mark := WithBPSClientDelivery(context.Background())
	ctx = WithBPSRequestState(ctx, []string{"bps"}, req, coreexecutor.Options{})
	a := &Auth{ID: "A", Provider: "bps", Metadata: map[string]any{"account_id": "a", "sub": "a"}}
	b := &Auth{ID: "B", Provider: "bps", Metadata: map[string]any{"account_id": "b", "sub": "b"}}
	if e := beginBPSDispatch(ctx, a, req, coreexecutor.Options{}); e != nil {
		t.Fatal(e)
	}
	if e := CheckBPSFailoverPayload(ctx, []byte(`{"input":"hi","metadata":{"task_id":"A"}}`)); e != nil {
		t.Fatal(e)
	}
	if !authorizeBPSFailover(ctx, failoverFacts{true, true}) {
		t.Fatal("missing authorization")
	}
	if e := beginBPSDispatch(ctx, b, req, coreexecutor.Options{}); e != nil {
		t.Fatal(e)
	}
	if e := CheckBPSFailoverPayload(ctx, []byte(`{"input":"changed","metadata":{"task_id":"B"}}`)); e == nil {
		t.Fatal("changed payload admitted")
	}
	mark()
	if e := CheckBPSFailoverPayload(ctx, []byte(`{"input":"hi","metadata":{"task_id":"B"}}`)); e == nil {
		t.Fatal("preflight client delivery ignored")
	}
}
