package kilobinding

import (
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloA1HintsThreadAndConflicts(t *testing.T) {
	meta := `{"thread_id":"child","session_id":"root","parent_thread_id":"root","history_initialization":"warm_fork"}`
	body := ojson.NewDocString(`{"prompt_cache_key":"root","client_metadata":{"thread_id":"child","session_id":"root"},"input":[{"type":"tool_search_call","execution":"client","call_id":"search","arguments":{}},{"type":"tool_search_output","execution":"client","call_id":"search","tools":[]}]}`)
	_ = body.Set("client_metadata.x-codex-turn-metadata", meta)
	headers := http.Header{}
	headers.Set("Thread-Id", "child")
	headers.Set("Session-Id", "root")
	headers.Set("X-Codex-Turn-Metadata", meta)
	h := ParseHints(headers, body.Bytes())
	if h.Err != nil || h.Session != "child" || h.Thread != "child" || h.Parent != "root" || h.Initialization != "warm_fork" || h.Kinds["search"] != "tool_search" || !h.Pairs["search"] {
		t.Fatalf("hints=%+v", h)
	}
	for _, field := range []string{"thread_id", "session_id", "history_initialization", "parent_thread_id"} {
		t.Run(field, func(t *testing.T) {
			conflict := ojson.NewDoc(body.Bytes())
			_ = conflict.Set("client_metadata."+field, "different")
			if got := ParseHints(headers, conflict.Bytes()); !errors.Is(got.Err, ErrConflict) {
				t.Fatal("conflicting metadata accepted")
			}
		})
	}
	legacy := ParseHints(http.Header{"Session-Id": {"legacy"}, "X-Session-Id": {"older"}}, []byte(`{"input":"new"}`))
	if legacy.Err != nil || legacy.Thread != "" || legacy.Session != "legacy" {
		t.Fatalf("legacy=%+v", legacy)
	}
}

func TestKiloA1ForkSnapshotOwnership(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	s := Store{Dir: t.TempDir(), Now: func() time.Time { return now }}
	identity := Identity{AuthID: "synthetic", Provider: "kilo", DeviceProfileID: "profile"}
	parent, err := s.Commit("caller", Record{}, identity, "root", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(parent, "resp-parent", map[string]string{"spawn": "function_call"}); err != nil {
		t.Fatal(err)
	}
	h := Hints{Thread: "child", Session: "child", Parent: "untrusted-control-parent", Initialization: "warm_fork", History: true, Calls: []string{"spawn"}, Kinds: map[string]string{"spawn": "function_call"}, Pairs: map[string]bool{"spawn": true}}
	plan, err := s.Resolve("caller", h)
	if err != nil || plan.ID != "" || plan.SourceID != parent.ID || plan.Identity != identity {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	child, err := s.Commit("caller", plan, identity, "child", false)
	if err != nil || child.ID == parent.ID || child.Session == parent.Session {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	root, _, _ := s.Lookup("caller", "session", "root")
	if !slices.Equal(root.Sessions, []string{"root"}) || root.Session != parent.Session {
		t.Fatal("parent alias/session changed")
	}
	restarted := Store{Dir: s.Dir, Now: s.Now}
	loaded, err := restarted.Resolve("caller", h)
	if err != nil || loaded.ID != child.ID || loaded.SourceID != parent.ID || loaded.InheritedCalls["spawn"] != "function_call" {
		t.Fatalf("restart=%+v err=%v", loaded, err)
	}
	if err := s.Publish(child, "resp-child", map[string]string{"child-call": "tool_search"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(parent, "resp-parent-later", map[string]string{"future": "function_call"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"future", "unknown"} {
		bad := Hints{Thread: "child", Session: "child", Calls: []string{id}, Kinds: map[string]string{id: "function_call"}, Pairs: map[string]bool{id: true}}
		if _, err := s.Resolve("caller", bad); !errors.Is(err, ErrConflict) {
			t.Fatalf("unlisted inherited id %s accepted: %v", id, err)
		}
	}
	orphan := h
	orphan.Pairs = map[string]bool{}
	if _, err := s.Resolve("caller", orphan); !errors.Is(err, ErrConflict) {
		t.Fatal("new inherited output-only accepted")
	}
	wrongKind := h
	wrongKind.Kinds = map[string]string{"spawn": "tool_search"}
	if _, err := s.Resolve("caller", wrongKind); !errors.Is(err, ErrConflict) {
		t.Fatal("changed public kind accepted")
	}
	if _, err := s.Resolve("other-caller", h); !errors.Is(err, ErrMissing) {
		t.Fatal("cross-caller fork accepted")
	}
	previous := h
	previous.Thread, previous.Session, previous.Previous = "another-child", "another-child", "resp-parent"
	if _, err := s.Resolve("caller", previous); !errors.Is(err, ErrConflict) {
		t.Fatal("parent previous_response_id fork accepted")
	}
	fresh, err := s.Resolve("caller", Hints{Thread: "fresh", Session: "fresh", Parent: "missing-parent", Initialization: "new"})
	if err != nil || fresh.SourceID != "" || fresh.Identity.AuthID != "" {
		t.Fatal("fresh child inherited control-parent credentials")
	}
	fresh, err = s.Commit("caller", fresh, identity, "fresh", false)
	if err != nil || fresh.Session == child.Session || fresh.Session == parent.Session {
		t.Fatal("fresh session was shared")
	}
	if err := s.Publish(fresh, "resp-sibling", map[string]string{"sibling": "function_call"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("caller", Hints{Thread: "child", Session: "child", Calls: []string{"sibling"}, Kinds: map[string]string{"sibling": "function_call"}, Pairs: map[string]bool{"sibling": true}}); !errors.Is(err, ErrConflict) {
		t.Fatal("sibling call accepted")
	}
	if err := s.Publish(child, "", map[string]string{"spawn": "function_call"}); !errors.Is(err, ErrConflict) {
		t.Fatal("inherited call ownership changed")
	}
	now = now.Add(30 * time.Minute)
	if _, err := s.Commit("caller", child, identity, "child", false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Minute)
	if _, err := s.Resolve("caller", h); !errors.Is(err, ErrMissing) {
		t.Fatalf("expired source accepted: %v", err)
	}
}
