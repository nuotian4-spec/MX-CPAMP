package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

// FetchBPSAccess uses only the dedicated CPA permission endpoint.
func FetchBPSAccess(ctx context.Context, client *http.Client, baseURL, key, authIndex string) (model.BPSAccess, error) {
	if strings.TrimSpace(authIndex) == "" {
		return model.BPSAccess{}, fmt.Errorf("missing_auth_index")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+"/v8/management/bps/credentials/access?auth_index="+url.QueryEscape(authIndex), nil)
	if err != nil {
		return model.BPSAccess{}, fmt.Errorf("request_error")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := localClient.Do(req)
	if err != nil {
		return model.BPSAccess{}, fmt.Errorf("request_error")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return model.BPSAccess{}, fmt.Errorf("management_http_%d", res.StatusCode)
	}
	var result model.BPSAccess
	decoder := json.NewDecoder(io.LimitReader(res.Body, 1024*1024))
	if decoder.Decode(&result) != nil || result.Provider != "bps" || result.AuthIndex != authIndex || result.Quota != nil || result.InferenceVerified || result.AutoRecoverEligible {
		return model.BPSAccess{}, fmt.Errorf("invalid_response")
	}
	if _, err = time.Parse(time.RFC3339, result.QueriedAt); err != nil {
		return model.BPSAccess{}, fmt.Errorf("invalid_response")
	}
	if result.Error == nil && (result.Allowed == nil || result.Models == nil || result.UpstreamStatus == nil || *result.UpstreamStatus != 200) {
		return model.BPSAccess{}, fmt.Errorf("invalid_response")
	}
	if result.Error != nil && (result.Allowed != nil || result.Models != nil) {
		return model.BPSAccess{}, fmt.Errorf("invalid_response")
	}
	return result, nil
}
