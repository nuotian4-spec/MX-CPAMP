package oauthlogin

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

// 仅批次使用既有凭证索引公开定位；真实文件名保留在内部单任务中。
func (s *Service) importedCredentialIndex(ctx context.Context, expected store.Setup, authID, provider string) (string, error) {
	checkConnection := func() error {
		current, ok, err := s.resolver.ResolveSetup(ctx)
		if err != nil || !ok {
			return ErrUnavailable
		}
		if !sameSetup(current, expected) {
			return ErrTargetChanged
		}
		return nil
	}
	if err := checkConnection(); err != nil {
		return "", err
	}
	var response struct {
		Files []struct {
			ID        string `json:"id"`
			Provider  string `json:"provider"`
			AuthIndex string `json:"auth_index"`
		} `json:"files"`
	}
	target := strings.TrimRight(expected.CPAUpstreamURL, "/") + "/v8/management/credentials?" + url.Values{"name": {authID}}.Encode()
	if err := s.request(ctx, http.MethodGet, target, expected.ManagementKey, nil, &response); err != nil {
		return "", err
	}
	if err := checkConnection(); err != nil {
		return "", err
	}
	index := ""
	for _, credential := range response.Files {
		if credential.ID != authID || credential.Provider != provider {
			continue
		}
		if index != "" || len(credential.AuthIndex) != 16 {
			return "", ErrUnavailable
		}
		if _, err := hex.DecodeString(credential.AuthIndex); err != nil {
			return "", ErrUnavailable
		}
		index = credential.AuthIndex
	}
	if index == "" {
		return "", ErrUnavailable
	}
	return index, nil
}
