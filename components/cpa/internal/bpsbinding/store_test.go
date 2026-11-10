package bpsbinding

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBindingRestartExpiryAndIdentityOnly(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	store := Store{Dir: t.TempDir(), Now: func() time.Time { return now }}
	identity := Identity{AuthID: "original.json", AccountID: "workspace", Subject: "user"}
	record, err := store.Save("caller", "call_native", identity)
	if err != nil {
		t.Fatal(err)
	}
	restarted := Store{Dir: store.Dir, Now: store.Now}
	got, err := restarted.Load("caller", record.CallID)
	if err != nil || got != record {
		t.Fatalf("restart lost binding: %+v %v", got, err)
	}
	if _, err = restarted.Load("another-caller", record.CallID); !errors.Is(err, ErrMissing) {
		t.Fatalf("cross-caller binding: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir, record.CallID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"arguments", "output", "input", "token", "password", "image"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("unexpected stored field %s", field)
		}
	}
	now = now.Add(Lifetime)
	if _, err = restarted.Load("caller", record.CallID); !errors.Is(err, ErrMissing) {
		t.Fatalf("expired binding accepted: %v", err)
	}
}

func TestBindingIDsReadOnlyProtocolFields(t *testing.T) {
	for _, raw := range []string{
		`{"input":[{"type":"function_call","call_id":"one","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":{"call_id":"not-a-protocol-id"}}]}`,
		`{"messages":[{"role":"assistant","tool_calls":[{"id":"one","type":"function","function":{"name":"x","arguments":"{}"}}]},{"role":"tool","tool_call_id":"one","content":"result"}]}`,
	} {
		ids := CallIDs([]byte(raw))
		if len(ids) != 1 || ids[0] != "one" {
			t.Fatalf("unexpected ids: %v", ids)
		}
	}
}
