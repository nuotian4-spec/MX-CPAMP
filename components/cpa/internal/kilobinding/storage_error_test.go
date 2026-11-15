package kilobinding

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKiloBindingSafeStorageDiagnostics(t *testing.T) {
	id := Identity{AuthID: "synthetic-account", Provider: "kilo"}
	t.Run("read", func(t *testing.T) {
		s := Store{Dir: t.TempDir()}
		if err := os.Mkdir(s.path("synthetic-private-caller"), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := s.Commit("synthetic-private-caller", Record{}, id, "private-session", false)
		op, kind, code, ok := StorageDiagnostic(err)
		if !ok || op != "read" || code == 0 || !errors.Is(err, ErrStorage) || err.Error() != ErrStorage.Error() {
			t.Fatal("read error not classified safely")
		}
		t.Logf("STORAGE_DIAGNOSTIC operation=%s error_type=%s system_code=%d", op, kind, code)
	})
	for _, test := range []struct{ name, body string }{{"validate_json", "private malformed content"}, {"validate_document", `{"caller_scope":"wrong","bindings":[]}`}, {"validate_record", `{"caller_scope":"synthetic-private-caller","bindings":[{}]}`}} {
		t.Run(test.name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			if err := os.WriteFile(s.path("synthetic-private-caller"), []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := s.Commit("synthetic-private-caller", Record{}, id, "private-session", false)
			op, kind, code, ok := StorageDiagnostic(err)
			if !ok || op != test.name || code != 0 || !errors.Is(err, ErrStorage) || err.Error() != ErrStorage.Error() {
				t.Fatal("validation error not classified safely")
			}
			for _, secret := range []string{s.Dir, test.body, "synthetic-private-caller"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("storage diagnostic leaked data")
				}
			}
			got, _ := os.ReadFile(s.path("synthetic-private-caller"))
			if string(got) != test.body {
				t.Fatal("validation failure replaced document")
			}
			temporary, _ := filepath.Glob(filepath.Join(s.Dir, ".binding-*"))
			if len(temporary) != 0 {
				t.Fatal("validation failure created temporary")
			}
			t.Logf("STORAGE_DIAGNOSTIC operation=%s error_type=%s system_code=%d", op, kind, code)
		})
	}
}
