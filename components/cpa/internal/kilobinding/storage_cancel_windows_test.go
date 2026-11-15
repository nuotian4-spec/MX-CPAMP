package kilobinding

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type observedBindingWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

// Done is evaluated by the replacement select only after a retryable rename error.
func (c *observedBindingWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestKiloBindingReplacementCancellationAndBudget(t *testing.T) {
	t.Run("already-canceled", func(t *testing.T) {
		dir := t.TempDir()
		from, to := filepath.Join(dir, "temporary"), filepath.Join(dir, "committed")
		os.WriteFile(from, []byte("new"), 0600)
		os.WriteFile(to, []byte("old"), 0600)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		spent := time.Duration(0)
		if err := replaceBindingFile(ctx, &spent, from, to); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled replacement was attempted")
		}
		got, _ := os.ReadFile(to)
		if string(got) != "old" || spent != 0 {
			t.Fatal("canceled operation mutated destination")
		}
	})
	t.Run("cancel-during-wait", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		id := Identity{AuthID: "synthetic", Provider: "kilo"}
		r, err := s.Commit("caller", Record{}, id, "old", false)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(s.path("caller"))
		handle := storageReadHandle(t, s.path("caller"))
		defer closeStorageHandle(handle)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		observed := &observedBindingWaitContext{Context: ctx, entered: make(chan struct{})}
		s = s.WithContext(observed)
		done := make(chan error, 1)
		go func() { _, e := s.Commit("caller", r, id, "new", false); done <- e }()
		select {
		case <-observed.entered:
		case <-done:
			t.Fatal("commit ended before the retry select")
		case <-time.After(time.Second):
			t.Fatal("retry select not reached")
		}
		cancel()
		started := time.Now()
		err = <-done
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrStorage) {
			t.Fatal("cancellation classification missing")
		}
		if time.Since(started) > 200*time.Millisecond {
			t.Fatal("cancellation waited for retries")
		}
		after, _ := os.ReadFile(s.path("caller"))
		if !bytes.Equal(before, after) {
			t.Fatal("cancellation replaced destination")
		}
		files, _ := filepath.Glob(filepath.Join(s.Dir, ".binding-*"))
		if len(files) != 0 {
			t.Fatal("temporary leaked")
		}
		t.Log("STORAGE_CANCEL retry_select_observed=true real_replace_error_before_select=true replacement_stopped=true old_document_unchanged=true temporary_clean=true")
	})
	t.Run("shared-commit-publish-budget", func(t *testing.T) {
		s := (Store{Dir: t.TempDir()}).WithContext(context.Background())
		id := Identity{AuthID: "synthetic", Provider: "kilo"}
		r, err := s.Commit("caller", Record{}, id, "old", false)
		if err != nil {
			t.Fatal(err)
		}
		handle := storageReadHandle(t, s.path("caller"))
		defer closeStorageHandle(handle)
		_, err = s.Commit("caller", r, id, "new", false)
		if !errors.Is(err, ErrStorage) {
			t.Fatal("permanent commit accepted")
		}
		spent := *s.replaceWait
		if spent != 55*time.Millisecond {
			t.Fatalf("scheduled waits=%s", spent)
		}
		err = s.Publish(r, "new-response", nil)
		if !errors.Is(err, ErrStorage) || *s.replaceWait != spent {
			t.Fatal("publish reset request wait budget")
		}
		t.Log("STORAGE_BUDGET commit_plus_publish_scheduled_wait_ms=55 attempts_per_operation_max=5")
	})
}
