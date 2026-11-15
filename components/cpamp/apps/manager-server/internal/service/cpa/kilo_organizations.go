package cpa

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var kiloOrganizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func FetchKiloOrganizations(ctx context.Context, client *http.Client, baseURL, key, id string) (model.KiloOrganizations, error) {
	var result model.KiloOrganizations
	fail := func(status int) (model.KiloOrganizations, error) { return result, &KiloWriteError{Status: status} }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, NormalizeBaseURL(baseURL)+"/v8/management/kilo/accounts/"+url.PathEscape(id)+"/organizations", nil)
	if err != nil {
		return fail(502)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	local := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		local = *client
	}
	transport := local.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if standard, ok := transport.(*http.Transport); ok {
		// 独立连接避免Go对复用连接上的GET自动重发profile查询。
		isolated := standard.Clone()
		isolated.DisableKeepAlives = true
		local.Transport = isolated
		defer isolated.CloseIdleConnections()
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fail(499)
		}
		return fail(503)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		switch res.StatusCode {
		case 400, 403, 404, 409, 499, 502, 503:
			return fail(res.StatusCode)
		default:
			return fail(502)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 128*1024+1))
	if ctx.Err() != nil {
		return fail(499)
	}
	if err != nil || len(raw) > 128*1024 {
		return fail(502)
	}
	var projection struct {
		Organizations *[]model.KiloOrganization `json:"organizations"`
		Personal      *bool                     `json:"has_personal_account"`
	}
	if json.Unmarshal(raw, &projection) != nil || projection.Organizations == nil || projection.Personal == nil {
		return fail(502)
	}
	for _, organization := range *projection.Organizations {
		if !kiloOrganizationIDPattern.MatchString(organization.ID) || utf8.RuneCountInString(organization.Name) > 256 {
			return fail(502)
		}
	}
	result.Organizations = *projection.Organizations
	result.HasPersonalAccount = *projection.Personal
	return result, nil
}
