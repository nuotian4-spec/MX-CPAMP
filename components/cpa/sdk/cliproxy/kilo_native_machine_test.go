package cliproxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type m4CountStore struct {
	coreauth.Store
	saves     atomic.Int32
	backfills atomic.Int32
}

func (s *m4CountStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	s.saves.Add(1)
	if raw, err := os.ReadFile(a.Attributes[coreauth.AttributePath]); err == nil && ojson.GetBytes(raw, "machine_id").String() == "" && coreauth.KiloMachineID(a) != "" {
		s.backfills.Add(1)
	}
	return s.Store.Save(ctx, a)
}

func m4MachineStream(seen chan string) http.HandlerFunc {
	var sequence atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("X-KILOCODE-MACHINEID")
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("machine-response-%d", sequence.Add(1)))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	}
}
func TestKiloMachineNewDistinctPersistentAndReplace(t *testing.T) {
	seen := make(chan string, 5)
	f := newKiloNativeFixture(t, m4MachineStream(seen), true)
	id := m2Create(t, f, "")
	other := m2Create(t, f, `,"enabled":false`)
	a, _ := f.manager.GetByID(id)
	b, _ := f.manager.GetByID(other)
	machine := coreauth.KiloMachineID(a)
	if machine == "" || machine == coreauth.KiloMachineID(b) || coreauth.KiloMachineID(b) == "" {
		t.Fatal("new accounts do not have independent UUIDs")
	}
	for round := 0; round < 3; round++ {
		if round == 1 {
			m2Reload(t, f)
		}
		if round == 2 {
			m2Request(t, f, "PATCH", kiloAccountsURL+"/"+id, `{"api_key":"synthetic-replaced-token"}`, 200)
			f.expectedToken.Store("synthetic-replaced-token")
		}
		status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"machine identity"}`)
		if status != 200 {
			t.Fatalf("inference=%d %s", status, body)
		}
		if header := <-seen; header != machine {
			t.Fatal("header changed across reload/key replacement")
		}
		raw, err := os.ReadFile(filepath.Join(f.cfg.AuthDir, id))
		if err != nil || ojson.GetBytes(raw, "machine_id").String() != machine {
			t.Fatal("header is not durable UUID")
		}
	}
}
func TestKiloMachineLegacyConcurrentBackfillAndStorageFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent", true: "storage-failure"}[fail], func(t *testing.T) {
			seen := make(chan string, 5)
			f := newKiloNativeFixture(t, m4MachineStream(seen), true)
			id := m2Create(t, f, "")
			path := filepath.Join(f.cfg.AuthDir, id)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc := ojson.NewDoc(raw)
			if err = doc.Delete("machine_id"); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, doc.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m2Reload(t, f)
			a, _ := f.manager.GetByID(id)
			if coreauth.KiloMachineID(a) != "" {
				t.Fatal("fixture did not load legacy account")
			}
			count := &m4CountStore{Store: m2FileStore(f)}
			f.manager.SetStore(count)
			if fail {
				f.manager.SetStore(m2FailStore{Store: count, save: true})
			}
			type outcome struct {
				status int
				body   []byte
			}
			done := make(chan outcome, 2)
			for i := 0; i < 2; i++ {
				go func() {
					status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"legacy machine"}`)
					done <- outcome{status, body}
				}()
			}
			want := 200
			if fail {
				want = 503
			}
			for i := 0; i < 2; i++ {
				result := <-done
				if result.status != want {
					t.Fatalf("inference=%d %s", result.status, result.body)
				}
			}
			if fail {
				if f.posts.Load() != 0 || count.saves.Load() != 0 {
					t.Fatal("failed persistence reached model")
				}
				return
			}
			first, second := <-seen, <-seen
			a, _ = f.manager.GetByID(id)
			raw, err = os.ReadFile(path)
			if err != nil || first == "" || first != second || first != coreauth.KiloMachineID(a) || first != ojson.GetBytes(raw, "machine_id").String() || count.backfills.Load() != 1 {
				t.Fatalf("legacy backfill mismatch: backfills=%d total saves=%d", count.backfills.Load(), count.saves.Load())
			}
			m2Reload(t, f)
			a, _ = f.manager.GetByID(id)
			if coreauth.KiloMachineID(a) != first {
				t.Fatal("legacy UUID lost after reload")
			}
		})
	}
}
