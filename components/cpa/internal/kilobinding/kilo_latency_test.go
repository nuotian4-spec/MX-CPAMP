package kilobinding

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
)

func TestKiloLatencyBindingOperations(t *testing.T) {
	ctx, state := kilolatency.NewContext(context.Background())
	state.Enable(true)
	store := (Store{Dir: t.TempDir()}).WithContext(ctx)
	record, err := store.Commit("caller", Record{}, Identity{AuthID: "account", Provider: "kilo"}, "session", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Publish(record, "response", map[string]string{"call": "function_call"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Lookup("caller", "response", "response"); err != nil || !ok {
		t.Fatalf("lookup=%t %v", ok, err)
	}
	fields := state.Finish(nil, 200)
	for _, name := range []string{"binding_lock_wait_ms", "binding_read_ms", "binding_commit_ms"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	if fields["binding_lock_wait_count"] != 3 {
		t.Fatalf("operation count=%v", fields)
	}
}
