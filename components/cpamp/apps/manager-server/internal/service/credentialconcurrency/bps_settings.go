package credentialconcurrency

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// BPSSettings uses the same connection reference and mutation fence as credentials.
type BPSSettings struct {
	Enabled           bool             `json:"enabled"`
	ImagesEnabled     bool             `json:"images_enabled"`
	ImageTransport    string           `json:"image_transport"`
	ImageRelayBaseURL string           `json:"image_relay_base_url"`
	Limits            map[string]int64 `json:"limits"`
	ConnectionRef     string           `json:"connection_ref"`
}

type BPSSettingsInput struct {
	Enabled           *bool  `json:"enabled"`
	ImagesEnabled     *bool  `json:"images_enabled"`
	ImageTransport    string `json:"image_transport"`
	ImageRelayBaseURL string `json:"image_relay_base_url"`
	ConnectionRef     string `json:"connection_ref"`
}

func (s *Service) GetBPSSettings(ctx context.Context) (BPSSettings, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return BPSSettings{}, err
	}
	var settings BPSSettings
	if _, err = s.request(ctx, setup, http.MethodGet, "/bps/settings", "", "", nil, &settings); err != nil {
		return BPSSettings{}, err
	}
	if settings.ImageTransport != "native" && settings.ImageTransport != "relay" {
		return BPSSettings{}, failure(502, "settings_invalid", "CPA的BPS设置回执不完整")
	}
	settings.ConnectionRef, err = s.bindConnection(ctx, setup)
	return settings, err
}

func (s *Service) PutBPSSettings(ctx context.Context, input BPSSettingsInput) error {
	if input.Enabled == nil || input.ImagesEnabled == nil || (input.ImageTransport != "native" && input.ImageTransport != "relay") {
		return failure(400, "settings_invalid", "请提交完整BPS开关与传输模式")
	}
	input.ImageRelayBaseURL = strings.TrimRight(strings.TrimSpace(input.ImageRelayBaseURL), "/")
	if input.ImageTransport == "relay" || input.ImageRelayBaseURL != "" {
		u, err := url.Parse(input.ImageRelayBaseURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return failure(400, "relay_origin_invalid", "中转模式需要不含路径、凭据、查询或片段的HTTPS基址")
		}
	}
	if len(input.ConnectionRef) != 32 {
		return failure(400, "connection_reference_required", "请刷新BPS设置后保存")
	}
	if _, err := hex.DecodeString(input.ConnectionRef); err != nil {
		return failure(400, "connection_reference_invalid", "连接引用无效，请刷新BPS设置")
	}
	release, err := s.mutations.AcquireAll(ctx)
	if err != nil {
		return failure(409, "mutation_busy", "CPA设置正在更新，请稍后刷新")
	}
	defer release()
	setup, err := s.setup(ctx)
	if err != nil {
		return err
	}
	if err = s.checkConnectionRef(input.ConnectionRef, setup); err != nil {
		return err
	}
	_, err = s.request(ctx, setup, http.MethodPut, "/bps/settings", "", input.ConnectionRef, map[string]any{"enabled": *input.Enabled, "images_enabled": *input.ImagesEnabled, "image_transport": input.ImageTransport, "image_relay_base_url": input.ImageRelayBaseURL}, nil)
	return err
}
