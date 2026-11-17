package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
)

func TestKiloLatencyAdmissionWaitClassification(t *testing.T) {
	for _, session := range []bool{false, true} {
		ctx, state := kilolatency.NewContext(context.Background())
		state.Enable(true)
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if session {
			ctx = kilolatency.WithSessionWait(ctx)
		}
		m := NewManager(nil, nil, nil)
		err := m.waitCredentialCapacity(ctx, make(chan struct{}), "pinned")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error=%v", err)
		}
		fields := state.Finish(err, 200)
		_, capacity := fields["capacity_wait_ms"]
		if capacity == session {
			t.Fatalf("session=%t capacity sample=%t", session, capacity)
		}
		if m.concurrency.waiting != 0 || len(m.concurrency.pinned) != 0 {
			t.Fatal("observation changed queue release")
		}
		if fields["outcome"] != "canceled" {
			t.Fatalf("outcome=%v", fields)
		}
	}
}

func TestKiloLatencyRejectedAfterKeepalive(t *testing.T) {
	_, state := kilolatency.NewContext(context.Background())
	state.Enable(true)
	state.Failure(concurrencyError("credential_concurrency_timeout"))
	if fields := state.Finish(nil, 200); fields["outcome"] != "rejected" || fields["status"] != 200 {
		t.Fatalf("committed heartbeat lost rejection: %v", fields)
	}
}
