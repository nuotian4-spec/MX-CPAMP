package auth

import (
	"context"
	"errors"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"testing"
	"time"
)

func permissionTestManager(t *testing.T, store CooldownStateStore) *Manager {
	t.Helper()
	m := NewManager(nil, nil, nil)
	m.SetCooldownStateStore(store)
	_, err := m.Register(WithSkipPersist(context.Background()), &Auth{ID: "permission-test", Provider: "bps", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func permissionTestFailure(m *Manager, model string, credential bool) {
	m.MarkResult(context.Background(), Result{AuthID: "permission-test", Provider: "bps", Model: model, Error: &Error{Code: ErrorCodeStreamTruncated, HTTPStatus: 502, Message: "bounded"}, PermissionDenial: &PermissionDenial{Source: "inband", UpstreamHTTPStatus: 200, SemanticStatus: 403, ReasonCode: "unknown", CredentialScope: credential}})
}

func TestBPSPermissionSuspensionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewFileCooldownStateStore(t.TempDir())
	m := permissionTestManager(t, store)
	permissionTestFailure(m, "model-a", false)
	permissionTestFailure(m, "model-b", false)
	before, _ := m.GetByID("permission-test")
	if len(before.PermissionSuspensions) != 2 {
		t.Fatalf("missing suspension: %+v", before.PermissionSuspensions)
	}
	if blocked, _, _ := isAuthBlockedForModel(before, "model-a", time.Now().Add(365*24*time.Hour)); !blocked {
		t.Fatal("permission suspension expired")
	}
	if blocked, _, _ := isAuthBlockedForModel(before, "model-c", time.Now()); blocked {
		t.Fatal("unrelated model blocked")
	}
	m.MarkResult(ctx, Result{AuthID: before.ID, Provider: "bps", Model: "model-a", Success: true, ExecutionSeq: 1})
	updated := before.Clone()
	updated.Metadata = map[string]any{"access_token": "synthetic-rotation"}
	if _, err := m.UpdateRefreshedAuth(WithSkipPersist(ctx), before, updated); err != nil {
		t.Fatal(err)
	}
	after, _ := m.GetByID(before.ID)
	if len(after.PermissionSuspensions) != 2 {
		t.Fatal("success or refresh cleared permission state")
	}
	if _, err := m.ReleasePermissionSuspensions(ctx, before.ID, "bps", before.RegistrationEpoch, before.CredentialVersion, before.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-a"}}); statusCodeFromError(err) != 409 {
		t.Fatalf("stale release: %v", err)
	}
	partial, err := m.ReleasePermissionSuspensions(ctx, after.ID, "bps", after.RegistrationEpoch, after.CredentialVersion, after.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-a"}})
	if err != nil || len(partial.PermissionSuspensions) != 1 {
		t.Fatalf("partial release failed: %v", err)
	}
	restarted := permissionTestManager(t, store)
	if err := restarted.RestoreCooldownStates(ctx); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.GetByID(before.ID)
	if len(restored.PermissionSuspensions) != 1 || restored.PermissionSuspensions[0].ModelKey != "model-b" || restored.RegistrationEpoch <= partial.RegistrationEpoch {
		t.Fatalf("restart lost state or CAS: %+v", restored)
	}
	if _, err := restarted.ReleasePermissionSuspensions(ctx, partial.ID, "bps", partial.RegistrationEpoch, partial.CredentialVersion, partial.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-b"}}); statusCodeFromError(err) != 409 {
		t.Fatalf("pre-restart CAS accepted: %v", err)
	}
	cleared, err := restarted.ReleasePermissionSuspensions(ctx, restored.ID, "bps", restored.RegistrationEpoch, restored.CredentialVersion, restored.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-b"}})
	if err != nil || len(cleared.PermissionSuspensions) != 0 {
		t.Fatalf("release failed: %v", err)
	}
	restartedAgain := permissionTestManager(t, store)
	if err := restartedAgain.RestoreCooldownStates(ctx); err != nil {
		t.Fatal(err)
	}
	final, _ := restartedAgain.GetByID(before.ID)
	if final.RegistrationEpoch <= cleared.RegistrationEpoch || len(final.PermissionSuspensions) != 0 {
		t.Fatal("empty state lost epoch watermark")
	}
}

type permissionFailedStore struct{ loadErr, saveErr error }

func (s permissionFailedStore) Load(context.Context) ([]CooldownStateRecord, error) {
	return nil, s.loadErr
}
func (s permissionFailedStore) Save(context.Context, []CooldownStateRecord) error { return s.saveErr }

func TestBPSPermissionSuspensionFailureAndIsolation(t *testing.T) {
	m := permissionTestManager(t, permissionFailedStore{saveErr: errors.New("synthetic")})
	permissionTestFailure(m, "model-a", true)
	auth, _ := m.GetByID("permission-test")
	if !permissionSuspended(auth, "new-model") || m.PermissionPersistenceStatus() != "error" {
		t.Fatal("save failure unblocked credential")
	}
	if _, err := m.ReleasePermissionSuspensions(context.Background(), auth.ID, "bps", auth.RegistrationEpoch, auth.CredentialVersion, auth.Generation, []PermissionScope{{Scope: "credential"}}); statusCodeFromError(err) != 503 {
		t.Fatalf("save failure release: %v", err)
	}
	still, _ := m.GetByID(auth.ID)
	if len(still.PermissionSuspensions) != 1 {
		t.Fatal("failed release cleared state")
	}
	other := auth.Clone()
	other.Provider = "codex"
	if permissionSuspended(other, "new-model") {
		t.Fatal("non-BPS policy changed")
	}
	m.SetCooldownStateStore(permissionFailedStore{loadErr: errors.New("synthetic")})
	if err := m.RestoreCooldownStates(context.Background()); err == nil || m.PermissionPersistenceStatus() != "unknown" {
		t.Fatal("load failure hidden")
	}
	if err := m.checkPermissionDispatch(auth, "new-model"); statusCodeFromError(err) != 503 {
		t.Fatalf("unknown allowed dispatch: %v", err)
	}
}

func TestBPSPermissionSuspensionDispatchRechecksLiveState(t *testing.T) {
	m := permissionTestManager(t, NewFileCooldownStateStore(t.TempDir()))
	selected, _ := m.GetByID("permission-test")
	permissionTestFailure(m, "model-a", false)
	if err := m.checkPermissionDispatch(selected, "model-a"); statusCodeFromError(err) != 403 {
		t.Fatalf("queued stale snapshot bypassed state: %v", err)
	}
	if err := m.checkPermissionDispatch(selected, "model-b"); err != nil {
		t.Fatalf("unrelated model blocked: %v", err)
	}
}

type permissionPartialBatchStore struct {
	*FileCooldownStateStore
	batchWrites  int
	rejectCommit bool
}

func (s *permissionPartialBatchStore) Save(ctx context.Context, records []CooldownStateRecord) error {
	s.batchWrites++
	if err := s.FileCooldownStateStore.Save(ctx, records); err != nil {
		return err
	}
	return errors.New("synthetic failure after batch commit")
}

func (s *permissionPartialBatchStore) SavePermissionRelease(ctx context.Context, id string, records []CooldownStateRecord) error {
	if s.rejectCommit {
		return errors.New("synthetic target replacement failure")
	}
	return s.FileCooldownStateStore.SavePermissionRelease(ctx, id, records)
}

func TestBPSPermissionSuspensionReleaseAtomicFailure(t *testing.T) {
	ctx := context.Background()
	store := NewFileCooldownStateStore(t.TempDir())
	m := permissionTestManager(t, store)
	permissionTestFailure(m, "model-a", false)
	auth, _ := m.GetByID("permission-test")
	fault := &permissionPartialBatchStore{FileCooldownStateStore: store, rejectCommit: true}
	m.SetCooldownStateStore(fault)
	_, err := m.ReleasePermissionSuspensions(ctx, auth.ID, "bps", auth.RegistrationEpoch, auth.CredentialVersion, auth.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-a"}})
	if statusCodeFromError(err) != 503 || fault.batchWrites != 0 {
		t.Fatalf("release used non-atomic batch: %v writes=%d", err, fault.batchWrites)
	}
	after, _ := m.GetByID(auth.ID)
	if len(after.PermissionSuspensions) != 1 {
		t.Fatal("503 cleared in-memory suspension")
	}
	restarted := permissionTestManager(t, store)
	if err := restarted.RestoreCooldownStates(ctx); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.GetByID(auth.ID)
	if len(restored.PermissionSuspensions) != 1 {
		t.Fatal("503 cleared persisted suspension")
	}
	fault.rejectCommit = false
	// The release bypasses a deliberately failing multi-file batch implementation.
	if _, err := m.ReleasePermissionSuspensions(ctx, after.ID, "bps", after.RegistrationEpoch, after.CredentialVersion, after.Generation, []PermissionScope{{Scope: "model", ModelKey: "model-a"}}); err != nil {
		t.Fatal(err)
	}
	if fault.batchWrites != 0 {
		t.Fatal("release invoked unrelated cleanup")
	}
}

func TestBPSPermissionSuspensionAliasAndTransportGate(t *testing.T) {
	m := permissionTestManager(t, NewFileCooldownStateStore(t.TempDir()))
	executor := &aliasRoutingExecutor{id: "bps"}
	m.RegisterExecutor(executor)
	m.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{"bps": {{Name: "model-a", Alias: "friendly", Fork: true}}})
	registry.GetGlobalRegistry().RegisterClient("permission-test", "bps", []*registry.ModelInfo{{ID: "friendly"}, {ID: "model-a"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient("permission-test") })
	auth, _ := m.GetByID("permission-test")
	ctx := m.permissionDispatchContext(context.Background(), auth, "model-a")
	permissionTestFailure(m, "model-a", false)
	if err := CheckBPSPermissionDispatch(ctx); statusCodeFromError(err) != 403 {
		t.Fatalf("late transport gate bypassed state: %v", err)
	}
	_, err := m.Execute(context.Background(), []string{"bps"}, cliproxyexecutor.Request{Model: "friendly"}, cliproxyexecutor.Options{})
	if err == nil || len(executor.ExecuteModels()) != 0 {
		t.Fatalf("alias bypassed suspension: %v %+v", err, executor.ExecuteModels())
	}
}

func TestBPSPermissionSuspensionOrdinaryMutationsPreserveState(t *testing.T) {
	m := permissionTestManager(t, NewFileCooldownStateStore(t.TempDir()))
	m.SetConfig(&internalconfig.Config{DisableCooling: true})
	permissionTestFailure(m, "model-a", false)
	auth, _ := m.GetByID("permission-test")
	if !permissionSuspended(auth, "model-a") {
		t.Fatal("disable_cooling bypassed permission state")
	}
	clearCooldownStateForAuth(auth, time.Now())
	if !permissionSuspended(auth, "model-a") {
		t.Fatal("ordinary cooldown reset cleared permission state")
	}
	auth.Disabled = true
	if _, err := m.Update(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatal(err)
	}
	auth, _ = m.GetByID(auth.ID)
	auth.Disabled = false
	auth.Status = StatusActive
	if _, err := m.Update(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatal(err)
	}
	auth, _ = m.GetByID(auth.ID)
	if !permissionSuspended(auth, "model-a") {
		t.Fatal("enable cleared permission state")
	}
}
