package config

import (
	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints/urlvalidator"
	"os"
	"path/filepath"
	"strings"
)

// BPSProtocolConfig控制新请求，在途请求继续使用原快照。
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
		if err := urlvalidator.ValidateHTTPSOrigin(base); err != nil {
			return fmt.Errorf("BPS图片中转需要有效的HTTPS基址，且不含凭据、路径、查询或片段")
		}
	}
	return nil
}

// BPSStateDirectory将运行时记录放在凭证扫描目录之外。
func BPSStateDirectory(cfg *Config) string {
	if cfg != nil {
		if path := strings.TrimSpace(cfg.BPSProtocol.StateDir); path != "" {
			return filepath.Clean(path)
		}
		if authDir := strings.TrimSpace(cfg.AuthDir); authDir != "" {
			authDir = expandBPSPath(authDir)
			return filepath.Join(filepath.Dir(filepath.Clean(authDir)), "bps-state")
		}
	}
	return filepath.Join("state", "bps")
}

func expandBPSPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimLeft(path[1:], "/\\"))
		}
	}
	return path
}

func (cfg *Config) ValidateBPSProtocol() error {
	if cfg == nil {
		return nil
	}
	if err := cfg.BPSProtocol.Validate(); err != nil {
		return err
	}
	if cfg.BPSProtocol.StateDir == "" {
		return nil
	}
	authDir := cfg.AuthDir
	if strings.TrimSpace(authDir) == "" {
		authDir = DefaultAuthDir
	}
	authPath, err := filepath.Abs(expandBPSPath(authDir))
	if err != nil {
		return err
	}
	statePath, err := filepath.Abs(expandBPSPath(cfg.BPSProtocol.StateDir))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(authPath, statePath)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("BPS状态目录必须位于凭证扫描目录之外")
	}
	return nil
}
