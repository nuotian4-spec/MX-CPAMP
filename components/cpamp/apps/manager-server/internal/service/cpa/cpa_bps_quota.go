package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchCPABPSQuota 只读插件SDK额度，并在查询前后核对凭据来源。
func FetchCPABPSQuota(ctx context.Context, client *http.Client, baseURL, key, authIndex, authID string) (model.CPABPSQuota, error) {
	empty := model.CPABPSQuota{}
	if strings.TrimSpace(authIndex) == "" {
		return empty, fmt.Errorf("missing_auth_index")
	}
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	get := func(path string, output any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+path, nil)
		if err != nil {
			return fmt.Errorf("request_error")
		}
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := local.Do(req)
		if err != nil {
			return fmt.Errorf("request_error")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("management_http_%d", res.StatusCode)
		}
		decoder := json.NewDecoder(io.LimitReader(res.Body, 1024*1024))
		if decoder.Decode(output) != nil {
			return fmt.Errorf("invalid_response")
		}
		return nil
	}
	identity := func() (string, error) {
		var response struct {
			Files []struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				AuthIndex string `json:"auth_index"`
				Provider  string `json:"provider"`
				Type      string `json:"type"`
			} `json:"files"`
		}
		if err := get("/v8/management/credentials", &response); err != nil {
			return "", err
		}
		id := ""
		matches := 0
		for _, f := range response.Files {
			if f.AuthIndex != authIndex {
				continue
			}
			matches++
			provider := f.Provider
			if provider == "" {
				provider = f.Type
			}
			candidate := f.ID
			if candidate == "" {
				candidate = f.Name
			}
			if provider != "cpa-bps" || candidate == "" || (authID != "" && authID != candidate) {
				return "", fmt.Errorf("credential_identity_mismatch")
			}
			id = candidate
		}
		if matches != 1 {
			return "", fmt.Errorf("credential_identity_mismatch")
		}
		return id, nil
	}
	bound, err := identity()
	if err != nil {
		return empty, err
	}
	var response struct {
		model.CPABPSQuotaData
		Provider  string `json:"provider"`
		AuthIndex string `json:"auth_index"`
		AuthID    string `json:"auth_id"`
		Source    string `json:"source"`
	}
	if err = get("/v8/management/plugins/cpa-bps/quota?"+url.Values{"auth_index": {authIndex}}.Encode(), &response); err != nil {
		return empty, err
	}
	if (response.Provider != "" && response.Provider != "cpa-bps") || (response.AuthIndex != "" && response.AuthIndex != authIndex) || (response.AuthID != "" && response.AuthID != bound) || (response.Source != "" && response.Source != "cpa_bps_plugin_quota") {
		return empty, fmt.Errorf("quota_identity_mismatch")
	}
	after, err := identity()
	if err != nil || after != bound {
		return empty, fmt.Errorf("credential_changed")
	}
	now := time.Now().UTC()
	q := response.CPABPSQuotaData
	if q.Groups == nil {
		q.Groups = []model.CPABPSQuotaGroup{}
	}
	for gi := range q.Groups {
		if q.Groups[gi].Buckets == nil {
			q.Groups[gi].Buckets = []model.CPABPSQuotaBucket{}
		}
		for bi := range q.Groups[gi].Buckets {
			b := &q.Groups[gi].Buckets[bi]
			if b.RemainingFraction != nil && (math.IsNaN(*b.RemainingFraction) || math.IsInf(*b.RemainingFraction, 0) || *b.RemainingFraction < 0 || *b.RemainingFraction > 1) {
				b.RemainingFraction = nil
			}
			if b.ResetTime != "" {
				at, e := time.Parse(time.RFC3339, b.ResetTime)
				if e != nil {
					b.ResetTime = ""
					b.RemainingFraction = nil
				} else if at.UnixMilli()-q.ServerTimeOffsetMs <= now.UnixMilli() {
					b.RemainingFraction = nil
				}
			}
		}
	}
	return model.CPABPSQuota{Provider: "cpa-bps", AuthIndex: authIndex, AuthID: bound, Source: "cpa_bps_plugin_quota", QueriedAt: now.Format(time.RFC3339Nano), Quota: q}, nil
}
