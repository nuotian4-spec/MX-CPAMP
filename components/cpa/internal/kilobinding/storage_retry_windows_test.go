package kilobinding

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func storageReadHandle(t *testing.T, path string) syscall.Handle {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}
func TestKiloBindingTransientReplacement(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	s := Store{Dir: t.TempDir(), Now: func() time.Time { return now }}
	identity := Identity{AuthID: "synthetic", Provider: "kilo"}
	r, err := s.Commit("synthetic-caller", Record{}, identity, "first", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(r, "response-one", map[string]string{"call-one": "function_call"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path("synthetic-caller"))
	if err != nil {
		t.Fatal(err)
	}
	handle := storageReadHandle(t, s.path("synthetic-caller"))
	defer func() {
		if handle != syscall.InvalidHandle {
			syscall.CloseHandle(handle)
		}
	}()
	type outcome struct {
		record Record
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := s.Commit("synthetic-caller", r, identity, "second", false); done <- outcome{r, e} }()
	// Keep the real reader open until the commit has created its temporary file.
	deadline := time.Now().Add(time.Second)
	for {
		select {
		case result := <-done:
			op, kind, code, _ := StorageDiagnostic(result.err)
			t.Fatalf("EARLY_REPLACEMENT_FAILURE operation=%s error_type=%s system_code=%d", op, kind, code)
		default:
		}
		files, _ := filepath.Glob(filepath.Join(s.Dir, ".binding-*"))
		if len(files) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("commit did not reach temporary file")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(25 * time.Millisecond)
	after, err := os.ReadFile(s.path("synthetic-caller"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("document changed while replacement was blocked")
	}
	if err = syscall.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	handle = syscall.InvalidHandle
	result := <-done
	if result.err != nil {
		op, kind, code, _ := StorageDiagnostic(result.err)
		t.Fatalf("TRANSIENT_REPLACEMENT_FAILURE operation=%s error_type=%s system_code=%d", op, kind, code)
	}
	restarted := Store{Dir: s.Dir, Now: s.Now}
	for _, lookup := range [][2]string{{"session", "first"}, {"session", "second"}, {"response", "response-one"}, {"call", "call-one"}} {
		got, ok, e := restarted.Lookup("synthetic-caller", lookup[0], lookup[1])
		if e != nil || !ok || got.ID != r.ID || got.Identity != identity || got.Session != r.Session || !got.Expires.Equal(result.record.Expires) {
			t.Fatal("restart ownership or expiry changed")
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, ".binding-*"))
	if len(files) != 0 {
		t.Fatal("temporary files retained")
	}
	t.Log("STORAGE_GREEN transient_replace=true old_document_intact_while_blocked=true restart_aliases=4 expiry_unchanged=true")
}
func TestKiloBindingPermanentReplacement(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	identity := Identity{AuthID: "synthetic", Provider: "kilo"}
	r, err := s.Commit("synthetic-caller", Record{}, identity, "first", false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path("synthetic-caller"))
	if err != nil {
		t.Fatal(err)
	}
	handle := storageReadHandle(t, s.path("synthetic-caller"))
	started := time.Now()
	_, err = s.Commit("synthetic-caller", r, identity, "second", false)
	elapsed := time.Since(started)
	syscall.CloseHandle(handle)
	op, kind, code, ok := StorageDiagnostic(err)
	if !errors.Is(err, ErrStorage) || !ok || op != "replace" || (code != 5 && code != 32) {
		t.Fatalf("permanent error classification %s %s %d", op, kind, code)
	}
	if elapsed > 2*time.Second {
		t.Fatal("replacement did not stop within bounded retry window")
	}
	after, readErr := os.ReadFile(s.path("synthetic-caller"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("permanent failure changed committed document")
	}
	restarted := Store{Dir: s.Dir}
	if got, ok, e := restarted.Lookup("synthetic-caller", "session", "first"); e != nil || !ok || got.ID != r.ID {
		t.Fatal("old alias lost")
	}
	if _, ok, e := restarted.Lookup("synthetic-caller", "session", "second"); e != nil || ok {
		t.Fatal("failed alias became visible")
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, ".binding-*"))
	if len(files) != 0 {
		t.Fatal("temporary files retained")
	}
	t.Logf("STORAGE_PERMANENT operation=%s error_type=%s system_code=%d elapsed_ms=%d old_document_unchanged=true", op, kind, code, elapsed.Milliseconds())
}
func closeStorageHandle(handle syscall.Handle) { _ = syscall.CloseHandle(handle) }
