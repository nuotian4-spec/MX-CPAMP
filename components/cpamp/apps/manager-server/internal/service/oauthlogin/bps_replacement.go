package oauthlogin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

var ErrBPSBuiltinReplaced = errors.New("BPS已切换为插件，请使用插件授权入口")

func (s *Service) BPSLoginMode(ctx context.Context) string {
	if s == nil || s.resolver == nil {
		return "unavailable"
	}
	setup, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok {
		return "unavailable"
	}
	mode, err := s.bpsLoginModeBound(ctx, setup)
	if err != nil {
		return "unavailable"
	}
	return mode
}

func (s *Service) bpsLoginModeBound(ctx context.Context, setup store.Setup) (string, error) {
	if strings.TrimSpace(setup.CPAUpstreamURL) == "" || setup.ManagementKey == "" {
		return "", ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(setup.CPAUpstreamURL, "/")+"/v8/management/config/plugins", nil)
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	res, err := s.client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer res.Body.Close()
	config := &struct {
		Configs map[string]struct {
			Enabled *bool `json:"enabled"`
		} `json:"configs"`
	}{}
	if res.StatusCode == http.StatusNotFound {
		var payload struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&payload) != nil || payload.Error != "not_found" {
			return "", ErrUnavailable
		}
	} else if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&config) != nil || config == nil {
		return "", ErrUnavailable
	}
	current, ok, err := s.resolver.ResolveSetup(ctx)
	if err != nil || !ok {
		return "", ErrUnavailable
	}
	if !sameSetup(current, setup) {
		return "", ErrTargetChanged
	}
	plugin := config.Configs["cpa-bps"]
	if plugin.Enabled != nil && *plugin.Enabled {
		return "plugin", nil
	}
	return "builtin", nil
}

func (s *Service) requireBuiltinBPS(ctx context.Context, setup store.Setup, provider string) error {
	if provider != "bps" {
		return nil
	}
	mode, err := s.bpsLoginModeBound(ctx, setup)
	if err != nil {
		return err
	}
	if mode == "plugin" {
		return ErrBPSBuiltinReplaced
	}
	return nil
}
