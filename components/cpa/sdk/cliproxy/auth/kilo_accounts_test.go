package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	kilotoken "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

type kiloMutationStore struct {
	entered, release       chan struct{}
	blockSave, blockDelete bool
}

func (s *kiloMutationStore) Save(_ context.Context, a *Auth) (string, error) {
	if s.blockSave {
		close(s.entered)
		<-s.release
	}
	path := a.Attributes[AttributePath]
	return path, (&kilotoken.TokenStorage{Metadata: a.Metadata}).SaveTokenToFile(path)
}
func (s *kiloMutationStore) Delete(_ context.Context, path string) error {
	if s.blockDelete {
		close(s.entered)
		<-s.release
	}
	return os.Remove(path)
}
func (s *kiloMutationStore) List(context.Context) ([]*Auth, error) { return nil, nil }
func TestKiloM2MutationFencesNewAdmissionThroughPublication(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "replace"
		if remove {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			store := &kiloMutationStore{}
			m := NewManager(store, nil, nil)
			m.SetConfig(&config.Config{AuthDir: filepath.Join(t.TempDir(), "auth")})
			key := "synthetic-old"
			a, err := m.ChangeKiloAccount(context.Background(), "", KiloAccountChanges{APIKey: &key}, true, false)
			if err != nil {
				t.Fatal(err)
			}
			store.entered = make(chan struct{})
			store.release = make(chan struct{})
			store.blockSave = !remove
			store.blockDelete = remove
			changed := make(chan error, 1)
			next := "synthetic-new"
			go func() {
				_, err := m.ChangeKiloAccount(context.Background(), a.ID, KiloAccountChanges{APIKey: &next}, false, remove)
				changed <- err
			}()
			select {
			case <-store.entered:
			case <-time.After(time.Second):
				t.Fatal("save/delete not entered")
			}
			// Deterministically observe the exact mutex shared by selection and AcquireCredentialGeneration.
			if m.concurrencyPickMu.TryLock() {
				m.concurrencyPickMu.Unlock()
				t.Fatal("admission fence released during persistence")
			}
			old, _ := m.GetByID(a.ID)
			if old.Metadata["api_key"] != "synthetic-old" {
				t.Fatal("runtime published before persistence")
			}
			acquired := make(chan error, 1)
			go func() {
				release, err := m.AcquireCredentialGeneration(context.Background(), a.ID)
				if release != nil {
					release()
				}
				acquired <- err
			}()
			close(store.release)
			if err := <-changed; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-acquired:
				if (err != nil) != remove {
					t.Fatalf("admission after %s: %v", name, err)
				}
			case <-time.After(time.Second):
				t.Fatal("admission did not resume")
			}
			current, ok := m.GetByID(a.ID)
			if remove {
				if ok {
					t.Fatal("deleted account available")
				}
			} else if !ok || current.Metadata["api_key"] != next || KiloIdentity(current).Revision != KiloIdentity(a).Revision+1 {
				t.Fatal("replacement missing")
			}
		})
	}
}
func TestKiloM2LegacyIdentityRevisionAndSameKey(t *testing.T) {
	store := &kiloMutationStore{}
	m := NewManager(store, nil, nil)
	m.SetConfig(&config.Config{AuthDir: t.TempDir()})
	key := "synthetic-key"
	a, err := m.ChangeKiloAccount(context.Background(), "", KiloAccountChanges{APIKey: &key}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	delete(a.Metadata, KiloIdentityRevisionKey)
	if KiloIdentity(a).Revision != 0 {
		t.Fatal("legacy revision must be zero")
	}
	m.mu.Lock()
	m.auths[a.ID] = a.Clone()
	m.mu.Unlock()
	a, err = m.ChangeKiloAccount(context.Background(), a.ID, KiloAccountChanges{APIKey: &key}, false, false)
	if err != nil || KiloIdentity(a).Revision != 0 {
		t.Fatal("same key changed legacy identity")
	}
	next := "synthetic-next"
	a, err = m.ChangeKiloAccount(context.Background(), a.ID, KiloAccountChanges{APIKey: &next}, false, false)
	if err != nil || KiloIdentity(a).Revision != 1 {
		t.Fatal("legacy manual replacement must advance revision")
	}
}
