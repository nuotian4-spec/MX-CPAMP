package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// BPSProtocolConfig controls new BPS requests. Active requests keep their snapshot.
type BPSProtocolConfig struct {
	Enabled           *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ImagesEnabled     *bool  `yaml:"images-enabled,omitempty" json:"images_enabled,omitempty"`
	ImageTransport    string `yaml:"image-transport,omitempty" json:"image_transport,omitempty"`
	ImageRelayBaseURL string `yaml:"image-relay-base-url,omitempty" json:"image_relay_base_url,omitempty"`
	StateDir          string `yaml:"state-dir,omitempty" json:"-"`
}

func (cfg *Config) IsBPSProtocolEnabled() bool {
	return cfg == nil || cfg.BPSProtocol.Enabled == nil || *cfg.BPSProtocol.Enabled
}

func (cfg *Config) IsBPSImagesEnabled() bool {
	return cfg == nil || cfg.BPSProtocol.ImagesEnabled == nil || *cfg.BPSProtocol.ImagesEnabled
}

func (c BPSProtocolConfig) Transport() string {
	if c.ImageTransport == "" {
		return "native"
	}
	return c.ImageTransport
}

func (c BPSProtocolConfig) Validate() error {
	if c.Transport() != "native" && c.Transport() != "relay" {
		return fmt.Errorf("BPS图片传输模式必须为native或relay")
	}
	base := strings.TrimSpace(c.ImageRelayBaseURL)
	if c.Transport() == "relay" || base != "" {
		u, err := url.Parse(base)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return fmt.Errorf("BPS图片中转需要有效的HTTPS基址，且不含凭据、路径、查询或片段")
		}
	}
	return nil
}

// BPSStateDirectory keeps runtime records outside the credential scanner tree.
func BPSStateDirectory(cfg *Config) string {
	if cfg != nil {
		if path := strings.TrimSpace(cfg.BPSProtocol.StateDir); path != "" {
			return filepath.Clean(path)
		}
		if authDir := strings.TrimSpace(cfg.AuthDir); authDir != "" {
			return filepath.Join(filepath.Dir(filepath.Clean(authDir)), "bps-state")
		}
	}
	return filepath.Join("state", "bps")
}
