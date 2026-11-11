package config

import "testing"

func TestBPSExplicitV8Catalog(t *testing.T) {
	input := []byte("config-version: 8\nserver:\n  port: 8317\noauth:\n  providers:\n    bps:\n      models: [configured-model]\n")
	if err := ValidateV8Config(input); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfigBytes(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.BPS.Models) != 1 || cfg.BPS.Models[0] != "configured-model" {
		t.Fatalf("catalog lost: %v", cfg.BPS.Models)
	}
	clone := cfg.CloneForRuntime()
	clone.BPS.Models[0] = "changed"
	if cfg.BPS.Models[0] != "configured-model" {
		t.Fatal("catalog shares mutable snapshot")
	}
	if len((&Config{}).BPS.Models) != 0 {
		t.Fatal("unexpected default catalog")
	}
}
