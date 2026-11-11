package helps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const BPSAccessURL = "https://bps.openai.com/basispoints/api/responses/access?include_models=true"
const bpsAccessBodyLimit = 2 << 20

var bpsAccessModelID = regexp.MustCompile(`^gpt-[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// BPSAccessObservation reports access permission independently of inference health.
type BPSAccessObservation struct {
	Provider            string   `json:"provider"`
	AuthIndex           string   `json:"auth_index"`
	AuthID              string   `json:"auth_id"`
	QueriedAt           string   `json:"queried_at"`
	Allowed             *bool    `json:"allowed"`
	Models              []string `json:"models"`
	UpstreamStatus      *int     `json:"upstream_status"`
	Error               *string  `json:"error"`
	Quota               *float64 `json:"quota"`
	InferenceVerified   bool     `json:"inference_verified"`
	AutoRecoverEligible bool     `json:"auto_recover_eligible"`
}

func (r *BPSAccessObservation) Fail(category string) {
	r.Allowed, r.Models, r.Error = nil, nil, &category
}

// QueryBPSAccess uses only the supplied credential snapshot and never refreshes it.
func QueryBPSAccess(parent context.Context, cfg *config.Config, auth *coreauth.Auth) (result BPSAccessObservation) {
	result.Provider = "bps"
	defer func() { result.QueriedAt = time.Now().UTC().Format(time.RFC3339) }()
	if auth == nil || auth.Provider != "bps" {
		result.Fail("credential_incomplete")
		return
	}
	result.AuthIndex, result.AuthID = auth.EnsureIndex(), auth.ID
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, BPSAccessURL, nil)
	token, _ := auth.Metadata["access_token"].(string)
	accountID, _ := auth.Metadata["account_id"].(string)
	if err := ApplyBPSHeaders(request, strings.TrimSpace(token), strings.TrimSpace(accountID)); err != nil {
		result.Fail("credential_incomplete")
		return
	}
	request.Header.Set("Accept", "application/json")
	client := NewProxyAwareHTTPClient(ctx, cfg, auth, 0)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		result.Fail(bpsAccessTransportError(ctx, err))
		return
	}
	if response == nil || response.Body == nil {
		result.Fail("invalid_response")
		return
	}
	defer func() { _ = response.Body.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = response.Body.Close() })
	defer stop()
	status := response.StatusCode
	result.UpstreamStatus = &status
	if status != http.StatusOK {
		category := "upstream_error"
		switch {
		case status == http.StatusUnauthorized:
			category = "unauthorized"
		case status == http.StatusForbidden:
			category = "forbidden"
		case status == http.StatusTooManyRequests:
			category = "rate_limited"
		case status >= 300 && status < 400:
			category = "redirect_rejected"
		}
		result.Fail(category)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, bpsAccessBodyLimit+1))
	if err != nil || ctx.Err() != nil {
		result.Fail(bpsAccessTransportError(ctx, err))
		return
	}
	allowed, models, valid := parseBPSAccess(raw)
	if len(raw) > bpsAccessBodyLimit || !valid {
		result.Fail("invalid_response")
		return
	}
	result.Allowed, result.Models = &allowed, models
	return
}

func bpsAccessTransportError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "transport_error"
}

func parseBPSAccess(raw []byte) (bool, []string, bool) {
	root := ojson.ParseBytes(raw)
	if !ojson.ValidBytes(raw) || !root.IsObject() || !root.Get("allowed").IsBool() {
		return false, nil, false
	}
	models := make([]string, 0)
	if !root.Get("allowed").Bool() {
		return false, models, true
	}
	catalog := root.Get("model_catalog")
	if !catalog.IsObject() || !catalog.Get("models").IsArray() {
		return false, nil, false
	}
	restricted := make(map[string]bool)
	if list := catalog.Get("restricted_models"); list.Exists() && list.Type != ojson.Null {
		if !list.IsArray() {
			return false, nil, false
		}
		for _, item := range list.Array() {
			if item.Type != ojson.String {
				return false, nil, false
			}
			restricted[item.String()] = true
		}
	}
	seen := make(map[string]bool)
	for _, entry := range catalog.Get("models").Array() {
		if !entry.IsObject() {
			return false, nil, false
		}
		id := entry.Get("id").String()
		if !bpsAccessModelID.MatchString(id) || restricted[id] || seen[id] {
			continue
		}
		enabled := true
		for _, key := range []string{"available", "enabled", "model_picker_enabled"} {
			value := entry.Get(key)
			if value.Exists() && value.Type != ojson.Null {
				if !value.IsBool() {
					return false, nil, false
				}
				enabled = enabled && value.Bool()
			}
		}
		policy := entry.Get("policy")
		if policy.Exists() && policy.Type != ojson.Null {
			if !policy.IsObject() || policy.Get("state").Exists() && policy.Get("state").Type != ojson.String && policy.Get("state").Type != ojson.Null {
				return false, nil, false
			}
		}
		state := policy.Get("state").String()
		if enabled && state != "disabled" && state != "unconfigured" {
			models = append(models, id)
			seen[id] = true
		}
	}
	return true, models, true
}
