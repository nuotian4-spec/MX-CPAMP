package kilo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKiloAtomicTokenPreservesExistingOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kilo.json")
	old := []byte(`{"api_key":"synthetic-old"}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	storage := &TokenStorage{Metadata: map[string]any{"api_key": "synthetic-new"}}
	called := false
	err := storage.save(path, func(tmp, target string) error {
		called = true
		content, e := os.ReadFile(tmp)
		if e != nil || !bytes.Contains(content, []byte("synthetic-new")) {
			t.Fatal("replacement was not completely written")
		}
		current, e := os.ReadFile(target)
		if e != nil || !bytes.Equal(current, old) {
			t.Fatal("old bytes changed before replacement")
		}
		return errors.New("synthetic rename failure")
	})
	if err == nil || !called {
		t.Fatal("rename failure not returned")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, old) {
		t.Fatal("old bytes lost after rename failure")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".kilo-credential-*"))
	if len(matches) != 0 {
		t.Fatal("temporary credential leaked")
	}
	storage.Metadata["invalid"] = make(chan int)
	if storage.SaveTokenToFile(path) == nil {
		t.Fatal("serialization failure not returned")
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, old) {
		t.Fatal("serialization failure changed old bytes")
	}
	delete(storage.Metadata, "invalid")
	if err = storage.SaveTokenToFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !bytes.Contains(data, []byte("synthetic-new")) {
		t.Fatal("replacement missing")
	}
}
