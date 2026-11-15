package cliproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type bindingUniqueResponse struct {
	http.ResponseWriter
	id string
}

func (w bindingUniqueResponse) Write(data []byte) (int, error) {
	_, err := w.ResponseWriter.Write(bytes.ReplaceAll(data, []byte("resp-kilo"), []byte(w.id)))
	return len(data), err
}
func (w bindingUniqueResponse) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

type bindingDiagnosticHook struct {
	mu      sync.Mutex
	entries []log.Fields
}

func (*bindingDiagnosticHook) Levels() []log.Level { return []log.Level{log.WarnLevel} }
func (h *bindingDiagnosticHook) Fire(e *log.Entry) error {
	if e.Message == "Kilo binding storage operation failed" {
		h.mu.Lock()
		defer h.mu.Unlock()
		copy := log.Fields{}
		for k, v := range e.Data {
			copy[k] = v
		}
		h.entries = append(h.entries, copy)
	}
	return nil
}
func TestKiloBindingStorageRealChain(t *testing.T) {
	logger := log.StandardLogger()
	oldOut := logger.Out
	hook := &bindingDiagnosticHook{}
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.AddHook(hook)
	logger.SetOutput(io.Discard)
	defer func() { logger.ReplaceHooks(oldHooks); logger.SetOutput(oldOut) }()
	for _, stream := range []bool{false, true} {
		for _, permanent := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/permanent=%t", stream, permanent), func(t *testing.T) {
				var sequence atomic.Int32
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					kiloSuccessStream(bindingUniqueResponse{ResponseWriter: w, id: fmt.Sprintf("resp-storage-%d", sequence.Add(1))}, r)
				})
				status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"seed"}`)
				if status != 200 {
					t.Fatal("seed request failed")
				}
				f.assertReleased(t, 1)
				files, err := filepath.Glob(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings", "*.json"))
				if err != nil || len(files) != 1 {
					t.Fatal("binding document missing")
				}
				path := files[0]
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				name, _ := syscall.UTF16PtrFromString(path)
				handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if handle != syscall.InvalidHandle {
						syscall.CloseHandle(handle)
					}
				}()
				posts, connects := f.posts.Load(), f.connects.Load()
				type result struct {
					status int
					data   []byte
				}
				done := make(chan result, 1)
				go func() {
					code, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":%t,"input":"independent turn","tools":[{"type":"function","name":"synthetic_tool","parameters":{"type":"object"}}]}`, stream))
					done <- result{code, data}
				}()
				if !permanent {
					deadline := time.Now().Add(time.Second)
					for {
						temporary, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".binding-*"))
						if len(temporary) > 0 {
							break
						}
						select {
						case <-done:
							t.Fatal("request completed before controlled reader release")
						default:
						}
						if time.Now().After(deadline) {
							t.Fatal("commit temporary not reached")
						}
						time.Sleep(time.Millisecond)
					}
					time.Sleep(20 * time.Millisecond)
					visible, _ := os.ReadFile(path)
					if !bytes.Equal(before, visible) {
						t.Fatal("blocked commit changed visible document")
					}
					if f.posts.Load() != posts {
						t.Fatal("upstream sent before durable commit")
					}
					syscall.CloseHandle(handle)
					handle = syscall.InvalidHandle
				}
				response := <-done
				f.assertReleased(t, 2)
				results := f.hook.snapshot()
				last := results[1]
				if permanent {
					if response.status != 503 || !bytes.Contains(response.data, []byte(kilobinding.ErrStorage.Error())) {
						t.Fatalf("storage response status=%d", response.status)
					}
					for _, private := range []string{path, ".binding-", "system_code", "replace", "synthetic-caller"} {
						if strings.Contains(string(response.data), private) {
							t.Fatal("public error leaked internal diagnostic")
						}
					}
					if f.posts.Load() != posts || f.connects.Load() != connects || last.Success {
						t.Fatal("failed storage issued POST, replayed or registered success")
					}
					after, _ := os.ReadFile(path)
					if !bytes.Equal(before, after) {
						t.Fatal("failed commit changed old document")
					}
					hook.mu.Lock()
					if len(hook.entries) == 0 {
						hook.mu.Unlock()
						t.Fatal("safe diagnostic missing")
					}
					entry := hook.entries[len(hook.entries)-1]
					hook.mu.Unlock()
					if len(entry) != 3 || entry["operation"] != "replace" || entry["error_type"] != "*os.LinkError" || (entry["system_code"] != uint64(5) && entry["system_code"] != uint64(32)) {
						t.Fatalf("safe diagnostic mismatch fields=%d", len(entry))
					}
				} else {
					if response.status != 200 || !bytes.Contains(response.data, []byte("kilo-ok")) || f.posts.Load() != posts+1 || f.connects.Load() != connects+1 || !last.Success || !last.CompletedResponse {
						t.Fatalf("recovered request status=%d posts_delta=%d", response.status, f.posts.Load()-posts)
					}
				}
				for _, a := range f.manager.List() {
					if a.Disabled || a.Unavailable || a.Status != coreauth.StatusActive || a.LastError != nil || !a.NextRetryAfter.IsZero() {
						t.Fatal("storage result changed account health")
					}
				}
				if f.unexpectedProxy.Load() != 0 {
					t.Fatal("unexpected proxy request")
				}
				temp, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".binding-*"))
				if len(temp) != 0 {
					t.Fatal("temporary file retained")
				}
				t.Logf("BINDING_CHAIN stream=%t permanent=%t status=%d POST_delta=%d CONNECT_delta=%d result_delta=1 account_egress=0 neutral=true unexpected_proxy=0", stream, permanent, response.status, f.posts.Load()-posts, f.connects.Load()-connects)
			})
		}
	}
}
