package config

import (
	"path/filepath"
	"testing"
)

func TestBPSProtocolDefaultsAndExplicitFalse(t *testing.T) {
	cfg := &Config{}
	if !cfg.IsBPSProtocolEnabled() || !cfg.IsBPSImagesEnabled() || cfg.BPSProtocol.Transport() != "native" {
		t.Fatal("BPS默认设置错误")
	}
	cfg, err := ParseConfigBytes([]byte("config-version: 8\nupstream:\n  bps:\n    protocol:\n      enabled: false\n      images-enabled: false\n      image-transport: native\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsBPSProtocolEnabled() || cfg.IsBPSImagesEnabled() {
		t.Fatal("显式关闭丢失")
	}
	clone := cfg.CloneForRuntime()
	*clone.BPSProtocol.Enabled = true
	if cfg.IsBPSProtocolEnabled() {
		t.Fatal("克隆与在途快照共享可变开关")
	}
}

func TestBPSProtocolRelayOriginAndStateIsolation(t *testing.T) {
	for _, origin := range []string{"", "http://images.example.com", "https://images.example.com/path", "https://user:pass@images.example.com", "https://images.example.com?", "https://images.example.com#", "https://images.example.com:99999"} {
		if err := (BPSProtocolConfig{ImageTransport: "relay", ImageRelayBaseURL: origin}).Validate(); err == nil {
			t.Errorf("接受无效基址 %q", origin)
		}
	}
	if err := (BPSProtocolConfig{ImageTransport: "relay", ImageRelayBaseURL: "https://images.example.com"}).Validate(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	auth := filepath.Join(root, "auths")
	for _, state := range []string{auth, filepath.Join(auth, "images")} {
		cfg := &Config{AuthDir: auth, BPSProtocol: BPSProtocolConfig{StateDir: state}}
		if cfg.ValidateBPSProtocol() == nil {
			t.Fatal("状态目录落入凭证扫描树", state)
		}
	}
	cfg := &Config{AuthDir: auth}
	if BPSStateDirectory(cfg) != filepath.Join(root, "bps-state") {
		t.Fatal(BPSStateDirectory(cfg))
	}
	cfg.BPSProtocol.StateDir = filepath.Join(root, "private-state")
	if err := cfg.ValidateBPSProtocol(); err != nil {
		t.Fatal(err)
	}
}
