package kilobinding

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKiloBindingLifetimeRestartAndIsolation(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	s := Store{Dir: t.TempDir(), Now: func() time.Time { return now }}
	id := Identity{AuthID: "synthetic-account", Provider: "kilo", AccountID: "synthetic-owner"}
	first, err := s.Commit("caller-a", Record{}, id, "session", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(first, "resp-one", map[string]string{"call-one": "function_call"}); err != nil {
		t.Fatal(err)
	}
	restarted := Store{Dir: s.Dir, Now: s.Now}
	got, ok, err := restarted.Lookup("caller-a", "response", "resp-one")
	if err != nil || !ok || got.Session != first.Session || !got.Expires.Equal(first.Expires) {
		t.Fatalf("restart=%+v %t %v", got, ok, err)
	}
	if _, ok, err = restarted.Lookup("caller-b", "response", "resp-one"); err != nil || ok {
		t.Fatalf("caller isolation=%t %v", ok, err)
	}
	now = now.Add(30 * time.Minute)
	renewed, err := s.Commit("caller-a", got, id, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.Expires.Equal(now.Add(Lifetime)) {
		t.Fatal("valid continuation did not renew TTL")
	}
	now = renewed.Expires
	for i := 0; i < 2; i++ {
		_, ok, err = s.Lookup("caller-a", "session", "session")
		if !ok || !errors.Is(err, ErrMissing) {
			t.Fatalf("expiry was forgotten: %t %v", ok, err)
		}
	}
	if _, err = s.Commit("caller-a", Record{}, id, "session", false); !errors.Is(err, ErrMissing) {
		t.Fatalf("expired recreated=%v", err)
	}
	fresh, err := s.Commit("caller-a", Record{}, id, "different-session", false)
	if err != nil || fresh.Session == first.Session {
		t.Fatalf("new session=%v", err)
	}
}
func TestKiloBindingStorageAndConflicts(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	id := Identity{AuthID: "a", Provider: "kilo", Organization: "org-a"}
	r, err := s.Commit("caller", Record{}, id, "one", false)
	if err != nil {
		t.Fatal(err)
	}
	changed := id
	changed.Organization = "org-b"
	if _, err = s.Commit("caller", r, changed, "", false); !errors.Is(err, ErrConflict) {
		t.Fatal("changed organization accepted")
	}
	if err = s.Publish(r, "response", map[string]string{"call": "function_call"}); err != nil {
		t.Fatal(err)
	}
	other, err := s.Commit("caller", Record{}, id, "two", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(other, "response", nil); !errors.Is(err, ErrConflict) {
		t.Fatal("response identity overwritten")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err = os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	broken := Store{Dir: path}
	if _, err = broken.Commit("caller", Record{}, id, "new", false); !errors.Is(err, ErrStorage) {
		t.Fatal("storage failure accepted")
	}
	raw, err := os.ReadFile(s.path("caller"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "token") || strings.Contains(string(raw), "active") {
		t.Fatal("non-binding data persisted")
	}
}
func TestKiloBindingHintsPriorityAndGate(t *testing.T) {
	h := http.Header{"Session-Id": []string{"first"}, "X-Session-Id": []string{"second"}}
	got := ParseHints(h, []byte(`{"previous_response_id":"response","prompt_cache_key":"third","input":[{"type":"function_call_output","call_id":"tool","output":"secret not indexed"}]}`))
	if got.Previous != "response" || got.Session != "first" || len(got.Calls) != 1 || got.Calls[0] != "tool" || !got.History {
		t.Fatalf("hints=%+v", got)
	}
	unlock, err := Lock(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := Lock(ctx, "key"); err == nil {
		release()
		t.Fatal("canceled gate admitted")
	}
	unlock()
	unlock()
	again, err := Lock(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestKiloBindingPreviousMovesSessionAliasAtomically(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	a, err := s.Commit("caller", Record{}, Identity{AuthID: "a", Provider: "kilo"}, "key-a", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Commit("caller", Record{}, Identity{AuthID: "b", Provider: "kilo"}, "key-b", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(a, "response-a", map[string]string{"call-a": "function_call"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(b, "response-b", map[string]string{"call-b": "function_call"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit("caller", a, a.Identity, "key-b", false); !errors.Is(err, ErrConflict) {
		t.Fatal("ordinary continuation stole session alias")
	}
	if _, err = s.Commit("caller", a, a.Identity, "key-b", true); err != nil {
		t.Fatal(err)
	}
	restarted := Store{Dir: s.Dir}
	r, ok, err := restarted.Lookup("caller", "session", "key-b")
	if err != nil || !ok || r.ID != a.ID {
		t.Fatalf("moved alias=%+v %v", r, err)
	}
	for _, kind := range []string{"response", "call"} {
		r, ok, err = restarted.Lookup("caller", kind, kind+"-b")
		if err != nil || !ok || r.ID != b.ID {
			t.Fatal("response/call ownership moved with session alias")
		}
	}
	records, err := s.read("caller")
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, r := range records {
		for _, key := range r.Sessions {
			if key == "key-b" {
				owners++
			}
		}
	}
	if owners != 1 {
		t.Fatalf("alias owners=%d", owners)
	}
}
func TestKiloBindingDeclarationHints(t *testing.T) {
	hints := ParseHints(nil, []byte(`{"input":[{"type":"function_call","call_id":"func-a"},{"type":"custom_tool_call","call_id":"custom-b"},{"type":"function_call_output","call_id":"func-a"}],"messages":[{"role":"assistant","tool_calls":[{"id":"chat-c","type":"function"}]}]}`))
	if !hints.History || len(hints.Calls) != 3 {
		t.Fatalf("declaration references=%+v", hints)
	}
}
