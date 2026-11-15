package bps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const ResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

// ResetCreditError exposes only a category; an uncertain consume result must not be retried.
type ResetCreditError struct {
	Code    string
	Unknown bool
}

func (e *ResetCreditError) Error() string { return e.Code }

func (c *Client) resetCreditRequest(ctx context.Context, token, accountID, method, suffix, body string) (*http.Response, error) {
	if c == nil || c.initErr != nil || c.httpClient == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(accountID) == "" || strings.ContainsAny(token+accountID, "\r\n") {
		return nil, &ResetCreditError{Code: "credential_incomplete"}
	}
	req, err := http.NewRequestWithContext(ctx, method, ResetCreditsURL+suffix, strings.NewReader(body))
	if err != nil {
		return nil, &ResetCreditError{Code: "invalid_request"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal")
	client := *c.httpClient
	client.Timeout = 0
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if transport, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && transport != nil {
		client.Transport = transport
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, &ResetCreditError{Code: "transport_error", Unknown: method == http.MethodPost}
	}
	if res == nil || res.Body == nil {
		return nil, &ResetCreditError{Code: "invalid_response", Unknown: method == http.MethodPost}
	}
	return res, nil
}

func (c *Client) ResetCreditsAvailable(ctx context.Context, token, accountID string) (int64, error) {
	res, err := c.resetCreditRequest(ctx, token, accountID, http.MethodGet, "", "")
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, &ResetCreditError{Code: resetCreditStatus(res.StatusCode)}
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	root := ojson.ParseBytes(raw)
	available := root.Get("available_count")
	count, parseErr := strconv.ParseInt(available.Raw, 10, 64)
	if readErr != nil || len(raw) > 1<<20 || !ojson.ValidBytes(raw) || !root.IsObject() || available.Type != ojson.Number || parseErr != nil || count < 0 {
		return 0, &ResetCreditError{Code: "invalid_response"}
	}
	return count, nil
}

func (c *Client) ConsumeResetCredit(ctx context.Context, token, accountID, requestID string) error {
	doc := ojson.NewDocString("{}")
	if err := doc.Set("redeem_request_id", requestID); err != nil {
		return &ResetCreditError{Code: "invalid_request"}
	}
	res, err := c.resetCreditRequest(ctx, token, accountID, http.MethodPost, "/consume", doc.String())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &ResetCreditError{Code: resetCreditStatus(res.StatusCode), Unknown: true}
	}
	return nil
}

func ResetCreditErrorCode(err error) string {
	var classified *ResetCreditError
	if errors.As(err, &classified) {
		return classified.Code
	}
	return "upstream_error"
}

func resetCreditStatus(status int) string {
	switch status {
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 429:
		return "rate_limited"
	default:
		return "upstream_error"
	}
}
