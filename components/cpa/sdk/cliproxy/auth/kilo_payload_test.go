package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type kiloPayloadPrepareSpy struct {
	mockCustomErrorExecutor
	calls int
}

func (s *kiloPayloadPrepareSpy) PrepareKiloPayload(ctx context.Context, _ core.Request, _ core.Options) (context.Context, error) {
	s.calls++
	return ctx, nil
}

func TestKiloPayloadPreparationIsolation(t *testing.T) {
	m := NewManager(nil, nil, nil)
	spy := &kiloPayloadPrepareSpy{mockCustomErrorExecutor: mockCustomErrorExecutor{identifier: "kilo"}}
	m.RegisterExecutor(spy)
	req := core.Request{Payload: []byte(strings.Repeat("x", 21<<20))}
	for _, scope := range []string{"codex", "bps", "", "kilo"} {
		for _, trusted := range []bool{false, true} {
			ctx := channelscope.WithKiloPayload(channelscope.With(context.Background(), scope), trusted)
			before := spy.calls
			if _, err := m.prepareKiloPayload(ctx, []string{"kilo"}, req, core.Options{}); err != nil {
				t.Fatal(err)
			}
			want := 0
			if scope == "kilo" && trusted {
				want = 1
			}
			if spy.calls-before != want {
				t.Fatalf("scope=%q trusted=%t calls=%d", scope, trusted, spy.calls-before)
			}
		}
	}
}

func TestKiloPayloadResultClassification(t *testing.T) {
	for _, sample := range []struct {
		provider, body string
		want           bool
	}{
		{"kilo", `{"upstream_status":413,"error":{"code":"too_large"}}`, true},
		{"kilo", `{"upstream_status":200,"error":{"code":"invalid_encrypted_content"}}`, true},
		{"kilo", `{"upstream_status":400,"error":{"code":"other_request_error"}}`, false},
		{"codex", `{"upstream_status":413,"error":{"code":"too_large"}}`, false},
		{"bps", `{"upstream_status":200,"error":{"code":"invalid_encrypted_content"}}`, false},
	} {
		result := Result{Provider: sample.provider, Error: &Error{Code: ErrorCodeForceCooldown, Message: sample.body}}
		if got := kiloPayloadRequestResult(result); got != sample.want {
			t.Fatalf("provider=%s body=%s neutral=%t want=%t", sample.provider, sample.body, got, sample.want)
		}
	}
}
