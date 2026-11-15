package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClientKeyScopesV8RoundTripAndValidation(t *testing.T) {
	raw := []byte("config-version: 8\naccess:\n  api-keys: [synthetic-kilo, synthetic-codex, legacy]\n  client-key-scopes:\n    - api-key: synthetic-kilo\n      channel: kilo\n    - api-key: synthetic-codex\n      channel: codex\n")
	if err := ValidateV8Config(raw); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ClientKeyScopes) != 2 {
		t.Fatalf("lost bindings: %+v", cfg.ClientKeyScopes)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = SaveConfigPreserveComments(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateV8Config(saved); err != nil {
		t.Fatal(err)
	}
	restored, err := ParseConfigBytes(saved)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.ClientKeyScopes, cfg.ClientKeyScopes) {
		t.Fatalf("round-trip=%+v", restored.ClientKeyScopes)
	}
	for _, invalid := range []string{
		strings.Replace(string(raw), "channel: kilo", "channel: wildcard", 1),
		strings.Replace(string(raw), "api-key: synthetic-kilo", "api-key: missing", 1),
		string(raw) + "    - api-key: synthetic-kilo\n      channel: bps\n",
	} {
		if ValidateV8Config([]byte(invalid)) == nil {
			t.Fatal("invalid scopes accepted by v8")
		}
		if _, err = ParseConfigBytes([]byte(invalid)); err == nil {
			t.Fatal("invalid scopes accepted by load")
		}
	}
}
