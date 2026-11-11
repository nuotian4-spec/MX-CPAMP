package bps

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// TokenStorage publishes a complete credential with an atomic rename.
type TokenStorage struct{ Metadata map[string]any }

func (s *TokenStorage) SetMetadata(metadata map[string]any) { s.Metadata = metadata }

func (s *TokenStorage) SaveTokenToFile(path string) error {
	doc := ojson.NewDoc([]byte("{}"))
	for key, value := range s.Metadata {
		key = strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:").Replace(key)
		if err := doc.Set(key, value); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bps-credential-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err = tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(doc.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
