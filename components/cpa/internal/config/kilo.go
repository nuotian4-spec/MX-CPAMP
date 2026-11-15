package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// KiloConfig controls the native proxy pool; credentials use existing auth files.
type KiloConfig struct {
	// ImageReductionEnabled permits bounded lossy resizing of inline images.
	ImageReductionEnabled bool        `yaml:"image-reduction-enabled" json:"image-reduction-enabled"`
	StateDir              string      `yaml:"state-dir,omitempty" json:"-"`
	ProxyLimit            int         `yaml:"proxy-limit" json:"proxy-limit"`
	Proxies               []KiloProxy `yaml:"proxies" json:"proxies"`
}

// KiloProxy is public unless AccountID binds it to one CPA credential.
type KiloProxy struct {
	ID        string `yaml:"id" json:"id"`
	URL       string `yaml:"url" json:"url,omitempty"`
	AccountID string `yaml:"account-id,omitempty" json:"account-id,omitempty"`
	Disabled  bool   `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

// KiloStateDirectory keeps continuation records outside credential discovery.
func KiloStateDirectory(cfg *Config) string {
	if cfg != nil && strings.TrimSpace(cfg.Kilo.StateDir) != "" {
		return resolveBPSPath(cfg.Kilo.StateDir)
	}
	return filepath.Join(filepath.Dir(bpsAuthDirectory(cfg)), "kilo-state")
}
func (cfg *Config) ValidateKiloState() error {
	rel, err := filepath.Rel(bpsAuthDirectory(cfg), KiloStateDirectory(cfg))
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Kilo state directory must be outside credential discovery")
	}
	return nil
}
