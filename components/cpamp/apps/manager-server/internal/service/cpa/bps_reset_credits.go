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

func FetchBPSResetCredits(ctx context.Context, client *http.Client, baseURL, key, authIndex, authID string) (model.BPSResetCredits, error) {
	empty := model.BPSResetCredits{}
	if strings.TrimSpace(authIndex) == "" || strings.TrimSpace(authID) == "" {
		return empty, fmt.Errorf("missing_credential_identity")
	}
	query := url.Values{"auth_index": {authIndex}, "auth_id": {authID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+"/v8/management/bps/credentials/quota?"+query.Encode(), nil)
	if err != nil {
		return empty, fmt.Errorf("request_error")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		return empty, fmt.Errorf("request_error")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return empty, fmt.Errorf("management_http_%d", res.StatusCode)
	}
	var raw struct {
		Provider  string   `json:"provider"`
		AuthID    string   `json:"auth_id"`
		AuthIndex string   `json:"auth_index"`
		Source    string   `json:"source"`
		QueriedAt string   `json:"queried_at"`
		Count     *float64 `json:"available_count"`
		Error     *string  `json:"query_error"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&raw) != nil || raw.Provider != "bps" || raw.AuthID != authID || raw.AuthIndex != authIndex || raw.Source != "chatgpt_account_reset_credits" {
		return empty, fmt.Errorf("invalid_response")
	}
	if _, err := time.Parse(time.RFC3339, raw.QueriedAt); err != nil {
		return empty, fmt.Errorf("invalid_response")
	}
	result := model.BPSResetCredits{Provider: raw.Provider, AuthID: authID, AuthIndex: authIndex, Source: raw.Source, QueryMode: "direct", QueriedAt: raw.QueriedAt, Status: "unknown"}
	if raw.Error != nil {
		code := "query_failed"
		result.Error = &code
		result.Status = "error"
	} else if raw.Count != nil && !math.IsNaN(*raw.Count) && !math.IsInf(*raw.Count, 0) && *raw.Count >= 0 && *raw.Count <= 9007199254740991 && math.Trunc(*raw.Count) == *raw.Count {
		result.Count = raw.Count
		result.Status = "known"
	}
	return result, nil
}
