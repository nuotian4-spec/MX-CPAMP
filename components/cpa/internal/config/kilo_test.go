package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestKiloConfigV8RoundTripAndRuntimeIsolation(t *testing.T) {
	raw := []byte("config-version: 8\nupstream:\n  kilo:\n    proxy-limit: 7\n    proxies:\n      - id: synthetic-public\n        url: http://synthetic-user:synthetic-pass@127.0.0.1:9000\n      - id: synthetic-private\n        url: socks5://127.0.0.1:9001\n        account-id: synthetic-account\n        disabled: true\n")
	if err := ValidateV8Config(raw); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kilo.ProxyLimit != 7 || len(cfg.Kilo.Proxies) != 2 || cfg.Kilo.Proxies[1].AccountID != "synthetic-account" {
		t.Fatalf("parsed config=%+v", cfg.Kilo)
	}
	clone := cfg.CloneForRuntime()
	clone.Kilo.Proxies[0].URL = "changed"
	if cfg.Kilo.Proxies[0].URL == "changed" {
		t.Fatal("runtime clone shares proxy slice")
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
	if !reflect.DeepEqual(restored.Kilo, cfg.Kilo) {
		t.Fatalf("round-trip lost Kilo config: %+v", restored.Kilo)
	}
}
