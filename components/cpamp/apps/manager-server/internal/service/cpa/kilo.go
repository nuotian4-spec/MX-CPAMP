package cpa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var ErrKiloManagementAuth = errors.New("Kilo管理连接认证失败")
var ErrKiloManagementRequest = errors.New("Kilo管理读取未确认")
var ErrKiloManagementResponse = errors.New("Kilo管理回执无效")

func FetchKiloAccounts(ctx context.Context, client *http.Client, baseURL, key string) (model.KiloAccounts, error) {
	var result model.KiloAccounts
	if err := fetchKilo(ctx, client, baseURL, key, "accounts", &result); err != nil {
		return model.KiloAccounts{}, err
	}
	result.ConnectionRef = ""
	if result.Accounts == nil {
		result.Accounts = []model.KiloAccount{}
	}
	for index := range result.Accounts {
		item := &result.Accounts[index]
		if !validKiloAccountSummary(item) {
			return model.KiloAccounts{}, ErrKiloManagementResponse
		}
		if item.ID == "" || item.EffectiveLimit < 1 || item.Active < 0 || item.Waiting < 0 || (item.ConcurrencyLimit != nil && *item.ConcurrencyLimit < 1) {
			return model.KiloAccounts{}, ErrKiloManagementResponse
		}
		switch item.QuotaSource {
		case "none", "codex_ref", "imported":
		default:
			return model.KiloAccounts{}, ErrKiloManagementResponse
		}
	}
	return result, nil
}

func FetchKiloStatus(ctx context.Context, client *http.Client, baseURL, key string) (model.KiloStatus, error) {
	var result model.KiloStatus
	if err := fetchKilo(ctx, client, baseURL, key, "status", &result); err != nil {
		return model.KiloStatus{}, err
	}
	result.ConnectionRef = ""
	if result.AccountCount < 0 || result.HealthyProxyCount < 0 || result.BusyProxyCount < 0 {
		return model.KiloStatus{}, ErrKiloManagementResponse
	}
	return result, nil
}

func FetchKiloCapabilities(ctx context.Context, client *http.Client, baseURL, key string) (model.KiloCapabilities, error) {
	var result model.KiloCapabilities
	if err := fetchKilo(ctx, client, baseURL, key, "capabilities", &result); err != nil {
		return model.KiloCapabilities{}, err
	}
	result.ConnectionRef = ""
	if result.Channel != "kilo" || !validKiloPublicProtocols(result.PublicProtocols) || result.ToolsSemantics != "source_parity" || result.ImagesSemantics != "source_parity" || result.AutoAccountProbeDefault || result.AutoModelProbeDefault || result.UpstreamBillingVerified {
		return model.KiloCapabilities{}, ErrKiloManagementResponse
	}
	return result, nil
}

func validKiloPublicProtocols(protocols []string) bool {
	if len(protocols) == 0 {
		return false
	}
	seen := make(map[string]bool, len(protocols))
	for _, protocol := range protocols {
		if (protocol != "responses" && protocol != "chat_completions") || seen[protocol] {
			return false
		}
		seen[protocol] = true
	}
	return true
}

func fetchKilo(ctx context.Context, client *http.Client, baseURL, key, resource string, output any) error {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(key) == "" {
		return ErrKiloManagementRequest
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+"/v8/management/kilo/"+resource, nil)
	if err != nil {
		return ErrKiloManagementRequest
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	local := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		local = *client
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		return ErrKiloManagementRequest
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return ErrKiloManagementAuth
	}
	if res.StatusCode != http.StatusOK {
		return ErrKiloManagementRequest
	}
	const maxResponse = 1024 * 1024
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return ErrKiloManagementResponse
	}
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") || json.Unmarshal(raw, output) != nil {
		return ErrKiloManagementResponse
	}
	return nil
}

func validKiloAccountSummary(account *model.KiloAccount) bool {
	if !validKiloClientIdentity(account.ClientIdentity) {
		return false
	}
	if account.SourcePolicy != nil && !validKiloSourcePolicy(account.SourcePolicy) {
		return false
	}
	if account.QuotaSnapshot == nil && account.QuotaSourceSummary == nil {
		return true
	}
	view, summary := account.QuotaSnapshot, account.QuotaSourceSummary
	if view == nil || summary == nil || view.AccountID != account.ID || view.BindingRevision != summary.BindingRevision || view.Source.Kind != summary.Kind || view.Source.Status != summary.Status || (summary.Kind == "cpa_codex" && view.Source.CredentialID != summary.CredentialID) || len(summary.DisplayLabel) > 2048 {
		return false
	}
	raw, err := json.Marshal(view)
	if err != nil || !validQuotaView(raw, view) {
		return false
	}
	view.ConnectionRef = ""
	return true
}

var kiloClientProfileRef = regexp.MustCompile(`^profile-[1-9][0-9]*$`)

func validKiloClientIdentity(identity *model.KiloClientIdentity) bool {
	if identity == nil {
		return true
	}
	if identity.Profiles == nil {
		return false
	}
	switch identity.State {
	case "pending", "invalid":
		return len(identity.Profiles) == 0
	case "persisted":
		if len(identity.Profiles) == 0 {
			return false
		}
	default:
		return false
	}
	seen := make(map[string]bool, len(identity.Profiles))
	for _, profile := range identity.Profiles {
		if !kiloClientProfileRef.MatchString(profile.Ref) || seen[profile.Ref] {
			return false
		}
		seen[profile.Ref] = true
		if profile.Exits == nil {
			continue
		}
		for _, exit := range *profile.Exits {
			switch exit.Kind {
			case "ip":
				if exit.IP == nil {
					return false
				}
				ip, err := netip.ParseAddr(*exit.IP)
				if err != nil || ip.Zone() != "" || ip.Unmap().String() != *exit.IP {
					return false
				}
			case "direct", "legacy":
				if exit.IP != nil {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}
