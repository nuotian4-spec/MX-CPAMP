package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
)

func TestKiloA2PrivateSearchContext(t *testing.T) {
	raw := []byte(`{"input":[{"role":"assistant","content":[{"type":"output_text","text":"synthetic history"}]}]}`)
	headers := http.Header{"Thread-Id": {"parent"}, "X-Kilo-Internal-Search": {"true"}}
	hints := kilobinding.ParseHints(headers, raw)
	store := kilobinding.Store{Dir: t.TempDir()}
	ordinary, err := kiloSearchBindingHints(context.WithValue(context.Background(), "kiloSearchContextKey", true), hints, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve("caller", ordinary); err == nil {
		t.Fatal("external marker bypassed original ownership")
	}
	ctx := WithKiloSearchContext(context.Background(), "parent")
	internal, err := kiloSearchBindingHints(ctx, hints, raw)
	if err != nil || internal.History || internal.Session == hints.Session || internal.Parent != "parent" {
		t.Fatalf("internal context=%+v err=%v", internal, err)
	}
	if _, err := store.Resolve("caller", internal); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"input":[{"type":"function_call","call_id":"unowned","name":"tool"}]}`,
		`{"previous_response_id":"unowned","input":[]}`,
		`{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`,
		`{"input":[{"type":"item_reference","id":"unowned"}]}`,
	} {
		if _, err := kiloSearchBindingHints(ctx, kilobinding.ParseHints(headers, []byte(body)), []byte(body)); err == nil {
			t.Fatal("nontext ownership bypassed")
		}
	}
}

func TestKiloA2SharedAdmissionBudget(t *testing.T) {
	ctx := WithSharedCredentialWaitBudget(context.Background())
	first := ctx.Value(concurrencyBudgetKey{}).(*concurrencyBudget)
	first.spent = 30 * time.Second
	next := withConcurrencyBudget(ctx)
	if next.Value(concurrencyBudgetKey{}) != first {
		t.Fatal("batch wait budget reset")
	}
	m := NewManager(nil, nil, nil)
	if err := m.waitCredentialCapacity(next, make(chan struct{}), ""); err == nil || !isConcurrencyError(err) {
		t.Fatalf("spent budget accepted: %v", err)
	}
	if _, deadline := ctx.Deadline(); deadline {
		t.Fatal("admission budget became inference deadline")
	}
}
