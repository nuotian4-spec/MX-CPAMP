package cpa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type KiloWriteError struct{ Status int }

func (e *KiloWriteError) Error() string { return "Kilo账号写入未确认" }

// WriteKiloAccount只发送一次请求；上游错误正文和额外字段均不返回调用者。
func WriteKiloAccount(ctx context.Context, client *http.Client, baseURL, key, method, id string, fields map[string]json.RawMessage) (model.KiloAccount, error) {
	var result model.KiloAccount
	var body io.Reader
	if method != http.MethodDelete {
		raw, err := json.Marshal(fields)
		if err != nil {
			return result, &KiloWriteError{Status: 400}
		}
		body = bytes.NewReader(raw)
	}
	path := NormalizeBaseURL(baseURL) + "/v8/management/kilo/accounts"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return result, &KiloWriteError{Status: 502}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	local := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		local = *client
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := local.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, &KiloWriteError{Status: 499}
		}
		return result, &KiloWriteError{Status: 503}
	}
	defer res.Body.Close()
	want := http.StatusOK
	if method == http.MethodPost {
		want = http.StatusCreated
	}
	if method == http.MethodDelete {
		want = http.StatusNoContent
	}
	if res.StatusCode != want {
		status := res.StatusCode
		switch status {
		case 400, 403, 404, 409, 499, 502, 503:
		default:
			status = 502
		}
		return result, &KiloWriteError{Status: status}
	}
	if method == http.MethodDelete {
		return result, nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if ctx.Err() != nil {
		return result, &KiloWriteError{Status: 499}
	}
	if err != nil || len(raw) > 1024*1024 || json.Unmarshal(raw, &result) != nil || result.ID == "" || result.EffectiveLimit < 1 || result.Active < 0 || result.Waiting < 0 {
		return model.KiloAccount{}, &KiloWriteError{Status: 502}
	}
	if !validKiloAccountSummary(&result) {
		return model.KiloAccount{}, &KiloWriteError{Status: 502}
	}
	switch result.QuotaSource {
	case "none", "codex_ref", "imported":
	default:
		return model.KiloAccount{}, &KiloWriteError{Status: 502}
	}
	return result, nil
}
