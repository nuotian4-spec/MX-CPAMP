package kilo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// TokenStorage atomically replaces a complete Kilo credential.
type TokenStorage struct{ Metadata map[string]any }

func (s *TokenStorage) SetMetadata(metadata map[string]any) { s.Metadata = metadata }
func (s *TokenStorage) SaveTokenToFile(path string) error   { return s.save(path, os.Rename) }
func (s *TokenStorage) save(path string, rename func(string, string) error) error {
	doc := ojson.NewDocString("{}")
	if s.Metadata == nil {
		doc = ojson.NewDocString("null")
	}
	for key, value := range s.Metadata {
		path := strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:").Replace(key)
		if path == "" {
			path = ":"
		}
		if err := doc.Set(path, value); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".kilo-credential-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	err = file.Chmod(0600)
	if err == nil {
		_, err = file.Write(doc.Bytes())
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = rename(file.Name(), path)
	}
	return err
}
