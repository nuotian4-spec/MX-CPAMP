package model

import "testing"

func TestBPSTargetSelectionKeepsLegacyDefault(t *testing.T) {
	old := DefaultCodexInspectionConfig()
	if len(old.TargetProviders()) != 1 || old.TargetProviders()[0] != "codex" {
		t.Fatal("旧默认扩大")
	}
	for _, targets := range [][]string{{"bps"}, {"bps", "xai", "codex", "bps"}} {
		cfg := old
		cfg.TargetTypes = targets
		cfg.TargetType = targets[0]
		if err := ValidateCodexInspectionConfig(cfg); err != nil {
			t.Fatal(err)
		}
		roundTrip := UnmarshalCodexInspectionSettings(MarshalCodexInspectionSettings(cfg))
		if !roundTrip.HasTargetProvider("bps") {
			t.Fatal("BPS丢失")
		}
	}
}
