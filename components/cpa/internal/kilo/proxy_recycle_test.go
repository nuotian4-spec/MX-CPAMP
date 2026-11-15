package kilo

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
)

func TestM3BRecycleStateCompatibilityAndValidation(t *testing.T) {
	store := ProxyStateStore{Dir: t.TempDir()}
	state := ProxyState{Revision: 1, Limit: 2, Proxies: []SavedProxy{{ID: "active", URL: "http://localhost:1234"}}, Accounts: map[string]AccountEgress{}}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(store.Dir, StateFileName)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	doc := ojson.NewDoc(raw)
	if err = doc.Delete("recycle"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, doc.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	got, managed, err := store.Load(ProxyState{})
	if err != nil || !managed || len(got.Recycle) != 0 || got.Proxies[0] != state.Proxies[0] {
		t.Fatalf("M3A state compatibility=%+v %t %v", got, managed, err)
	}
	state.Recycle = []ArchivedProxy{{Proxy: SavedProxy{ID: "archived", URL: "http://private-user:private-pass@localhost:5678", Name: "saved", AccountID: "old-account", Order: 4, Disabled: true, FirstUsed: 7}, ArchivedAt: time.Date(2026, 11, 13, 4, 0, 0, 123, time.UTC)}}
	if err = store.Save(state); err != nil {
		t.Fatal(err)
	}
	got, _, err = store.Load(ProxyState{})
	if err != nil || !reflect.DeepEqual(got.Recycle, state.Recycle) {
		t.Fatalf("archive secret metadata roundtrip=%+v %v", got.Recycle, err)
	}
	raw, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"id-conflict", "recycle.0.id", "active"},
		{"bad-time", "recycle.0.archived_at", "not-time"},
		{"direct", "recycle.0.url", "direct"},
		{"bad-list", "recycle", true},
		{"archived-primary", "accounts", map[string]any{"a": map[string]any{"mode": "pool", "primary_proxy_id": "archived"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := ojson.NewDoc(raw)
			if err := doc.Set(tc.field, tc.value); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Load(state); !errors.Is(err, ErrStateStorage) {
				t.Fatalf("invalid archive loaded: %v", err)
			}
		})
	}
	copy := state.Clone()
	copy.Recycle[0].Proxy.Name = "changed"
	if state.Recycle[0].Proxy.Name != "saved" {
		t.Fatal("clone shares archive backing array")
	}
}
