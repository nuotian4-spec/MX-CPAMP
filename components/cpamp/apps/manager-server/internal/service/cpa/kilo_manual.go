package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

func kiloManualRequest(ctx context.Context, client *http.Client, baseURL, key, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, NormalizeBaseURL(baseURL)+"/v8/management/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, &KiloWriteError{Status: 502}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	local := http.Client{}
	if client != nil {
		local = *client
	}
	// 手动生成由请求上下文取消；账号等待预算由Core管理，不另加推理总超时。
	local.Timeout = 0
	if method == http.MethodGet {
		local.Timeout = 30 * time.Second
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &KiloWriteError{Status: 499}
		}
		return nil, &KiloWriteError{Status: 503}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		status := res.StatusCode
		switch status {
		case 400, 404, 409, 429, 499, 503:
		default:
			status = 502
		}
		return nil, &KiloWriteError{Status: status}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024+1))
	if ctx.Err() != nil {
		return nil, &KiloWriteError{Status: 499}
	}
	if err != nil || len(raw) > 4*1024*1024 {
		return nil, &KiloWriteError{Status: 502}
	}
	return raw, nil
}

func FetchKiloTestModels(ctx context.Context, client *http.Client, baseURL, key string) (model.KiloTestModels, error) {
	var result model.KiloTestModels
	raw, err := kiloManualRequest(ctx, client, baseURL, key, "GET", "routing/model-definitions/kilo", nil)
	if err != nil {
		return result, err
	}
	var catalog struct {
		Channel string                `json:"channel"`
		Models  []model.KiloTestModel `json:"models"`
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Channel != "kilo" || len(catalog.Models) == 0 {
		return result, &KiloWriteError{Status: 502}
	}
	seen := map[string]bool{}
	for _, item := range catalog.Models {
		if strings.TrimSpace(item.ID) == "" || seen[item.ID] || item.ID == "kilo-auto/free" {
			return result, &KiloWriteError{Status: 502}
		}
		seen[item.ID] = true
	}
	result.Models = catalog.Models
	return result, nil
}

func TestKiloAccount(ctx context.Context, client *http.Client, baseURL, key, id, modelID, prompt string) (model.KiloManualTest, error) {
	var result model.KiloManualTest
	body, _ := json.Marshal(map[string]string{"model": modelID, "prompt": prompt})
	raw, err := kiloManualRequest(ctx, client, baseURL, key, "POST", "kilo/accounts/"+url.PathEscape(id)+"/test", body)
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.Model != modelID || result.DurationMS < 0 {
		return model.KiloManualTest{}, &KiloWriteError{Status: 502}
	}
	if _, err = time.Parse(time.RFC3339Nano, result.StartedAt); err != nil {
		return model.KiloManualTest{}, &KiloWriteError{Status: 502}
	}
	if _, err = time.Parse(time.RFC3339Nano, result.CompletedAt); err != nil {
		return model.KiloManualTest{}, &KiloWriteError{Status: 502}
	}
	code := ""
	switch result.Status {
	case "completed":
		result.ErrorCode = nil
	case "incomplete":
		code = "incomplete"
	case "failed":
		code = "request_failed"
		result.Text = ""
	case "canceled":
		code = "canceled"
		result.Text = ""
	default:
		return model.KiloManualTest{}, &KiloWriteError{Status: 502}
	}
	if code != "" {
		result.ErrorCode = &code
	}
	result.AccountID = ""
	result.ConnectionRef = ""
	return result, nil
}
