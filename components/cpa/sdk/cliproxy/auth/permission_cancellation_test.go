package auth

import (
	"context"
	"errors"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type permissionCancellationEvidence struct{}

func (permissionCancellationEvidence) Error() string { return "bounded native refusal" }
func (permissionCancellationEvidence) BPSPermissionDenial() (string, int, bool) {
	return "inband", 200, false
}

func TestBPSPermissionCancellationBeforeTerminalDelivery(t *testing.T) {
	store := NewFileCooldownStateStore(t.TempDir())
	m := permissionTestManager(t, store)
	auth, _ := m.GetByID("permission-test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	_, _, err := readStreamBootstrap(ctx, chunks)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation before terminal delivery: %v", err)
	}
	m.recordCanceledUpstreamCooldown(ctx, auth, "bps", "model-a", "model-a", cliproxyexecutor.Options{}, err, false, 1)
	chunks <- cliproxyexecutor.StreamChunk{Err: permissionCancellationEvidence{}}
	close(chunks)
	m.drainCanceledPermissionStream(ctx, auth, "bps", "model-a", "model-a", cliproxyexecutor.Options{}, chunks, false, 1)
	state, _ := m.GetByID(auth.ID)
	if len(state.PermissionSuspensions) != 1 || state.Generation != auth.Generation+1 {
		t.Fatalf("late terminal was not recorded exactly once: %+v", state)
	}
	restarted := permissionTestManager(t, store)
	if err := restarted.RestoreCooldownStates(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.GetByID(auth.ID)
	if len(restored.PermissionSuspensions) != 1 {
		t.Fatal("late terminal did not persist")
	}
}

func TestBPSPermissionCancellationRequiresTypedEvidence(t *testing.T) {
	for _, err := range []error{context.Canceled, errors.New("HTTP 403 permission denied"), &Error{HTTPStatus: 403, Message: "permission denied"}} {
		m := permissionTestManager(t, nil)
		auth, _ := m.GetByID("permission-test")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m.recordCanceledUpstreamCooldown(ctx, auth, "bps", "model-a", "model-a", cliproxyexecutor.Options{}, err, false, 1)
		chunks := make(chan cliproxyexecutor.StreamChunk, 1)
		chunks <- cliproxyexecutor.StreamChunk{Err: err}
		close(chunks)
		m.drainCanceledPermissionStream(ctx, auth, "bps", "model-a", "model-a", cliproxyexecutor.Options{}, chunks, false, 1)
		state, _ := m.GetByID(auth.ID)
		if len(state.PermissionSuspensions) != 0 || state.Generation != auth.Generation {
			t.Fatalf("untyped cancellation changed state: %v", err)
		}
	}
}

func TestBPSPermissionCancellationPreservesBufferedTerminal(t *testing.T) {
	store := NewFileCooldownStateStore(t.TempDir())
	m := permissionTestManager(t, store)
	auth, _ := m.GetByID("permission-test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remaining := make(chan cliproxyexecutor.StreamChunk)
	close(remaining)
	result := m.wrapStreamResult(ctx, auth, "bps", "model-a", "model-a", nil,
		[]cliproxyexecutor.StreamChunk{{Err: permissionCancellationEvidence{}}}, remaining,
		OAuthModelAliasResult{}, false, cliproxyexecutor.Options{}, 1)
	for range result.Chunks {
	}
	state, _ := m.GetByID(auth.ID)
	if len(state.PermissionSuspensions) != 1 || state.Generation != auth.Generation+1 {
		t.Fatalf("buffered terminal was not recorded exactly once: %+v", state)
	}
	restarted := permissionTestManager(t, store)
	if err := restarted.RestoreCooldownStates(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.GetByID(auth.ID)
	if len(restored.PermissionSuspensions) != 1 {
		t.Fatal("buffered terminal did not persist")
	}
}
