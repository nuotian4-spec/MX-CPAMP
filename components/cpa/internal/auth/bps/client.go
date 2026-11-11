// Package bps implements the official Excel OAuth profile independently of Codex.
package bps

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

const (
	ClientID     = "app_fnr0pYvVwwFDocDumLG3H2Bp"
	RedirectURI  = "https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback"
	AuthorizeURL = "https://auth.openai.com/api/accounts/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token?unified=true"
)

type Client struct {
	httpClient *http.Client
	initErr    error
}

type OAuthError struct {
	Code   string
	status int
}

func (e *OAuthError) Error() string   { return "BPS OAuth " + e.Code }
func (e *OAuthError) StatusCode() int { return e.status }

func NewClient(proxyURL string) *Client {
	transport, _, err := proxyutil.BuildHTTPTransport(strings.TrimSpace(proxyURL))
	return &Client{httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second}, initErr: err}
}

func NewPKCE() (state, verifier, challenge string, err error) {
	random := make([]byte, 64)
	if _, err = rand.Read(random); err != nil {
		return
	}
	state = base64.RawURLEncoding.EncodeToString(random[:32])
	verifier = base64.RawURLEncoding.EncodeToString(random[32:])
	digest := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	return
}

func AuthorizationURL(state, challenge string) string {
	q := url.Values{"client_id": {ClientID}, "response_type": {"code"}, "redirect_uri": {RedirectURI}, "scope": {"openid offline_access email profile organization.read"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "audience": {"https://api.openai.com/v1"}, "platform": {"PC"}}
	return AuthorizeURL + "?" + q.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (map[string]any, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(verifier) == "" {
		return nil, errors.New("BPS OAuth code and PKCE are required")
	}
	return c.request(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {ClientID}, "code": {code}, "redirect_uri": {RedirectURI}, "code_verifier": {verifier}}, nil)
}

func (c *Client) Refresh(ctx context.Context, metadata map[string]any) (map[string]any, error) {
	if value, _ := metadata["client_id"].(string); value != ClientID {
		return nil, errors.New("BPS OAuth client mismatch")
	}
	if value, _ := metadata["profile"].(string); value != "" && value != "excel" {
		return nil, errors.New("BPS OAuth profile mismatch")
	}
	refresh, _ := metadata["refresh_token"].(string)
	if strings.TrimSpace(refresh) == "" {
		return nil, &OAuthError{Code: "invalid_grant", status: http.StatusUnauthorized}
	}
	return c.request(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {ClientID}, "refresh_token": {refresh}}, metadata)
}

func (c *Client) request(ctx context.Context, values url.Values, previous map[string]any) (map[string]any, error) {
	if c.initErr != nil {
		return nil, errors.New("BPS OAuth proxy configuration is invalid")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, errors.New("BPS OAuth request is invalid")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := *c.httpClient
	if transport, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && transport != nil {
		client.Transport = transport
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("BPS OAuth transport failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, errors.New("BPS OAuth response read failed")
	}
	doc := ojson.NewDoc(body)
	if resp.StatusCode != http.StatusOK {
		code := "upstream_error"
		status := resp.StatusCode
		if status == http.StatusUnauthorized || doc.Get("error").String() == "invalid_grant" {
			code = "invalid_grant"
			status = http.StatusUnauthorized
		} else if doc.Get("error").String() == "invalid_client" {
			code = "invalid_client"
		}
		return nil, &OAuthError{Code: code, status: status}
	}
	if !ojson.ValidBytes(body) || !ojson.ParseBytes(body).IsObject() {
		return nil, errors.New("BPS OAuth response is invalid")
	}
	access := strings.TrimSpace(doc.Get("access_token").String())
	expires := doc.Get("expires_in").Int()
	if access == "" || expires <= 0 {
		return nil, errors.New("BPS OAuth response has incomplete tokens")
	}
	result := make(map[string]any, len(previous)+7)
	for key, value := range previous {
		result[key] = value
	}
	result["access_token"] = access
	if refresh := strings.TrimSpace(doc.Get("refresh_token").String()); refresh != "" {
		result["refresh_token"] = refresh
	}
	if refresh, _ := result["refresh_token"].(string); strings.TrimSpace(refresh) == "" {
		return nil, errors.New("BPS OAuth response has no refresh token")
	}
	result["type"] = "bps"
	result["profile"] = "excel"
	result["client_id"] = ClientID
	result["expired"] = time.Now().UTC().Add(time.Duration(expires) * time.Second).Format(time.RFC3339)
	result["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	delete(result, "id_token")
	if idToken := doc.Get("id_token").String(); idToken != "" {
		claims, errClaims := Claims(idToken)
		if errClaims != nil {
			return nil, errClaims
		}
		for key, value := range claims {
			if value != "" {
				if old, _ := previous[key].(string); old != "" && old != value {
					return nil, errors.New("BPS OAuth refreshed identity mismatch")
				}
				result[key] = value
			}
		}
	}
	if strings.Count(access, ".") == 2 {
		claims, errClaims := accessClaims(access)
		if errClaims != nil {
			return nil, errClaims
		}
		for key, value := range claims {
			if value == "" {
				continue
			}
			if key != "expired" {
				if old, _ := result[key].(string); old != "" && old != value {
					return nil, errors.New("BPS OAuth refreshed identity mismatch")
				}
			}
			result[key] = value
		}
	}
	return result, nil
}

func accessClaims(token string) (map[string]string, error) {
	parts := strings.Split(token, ".")
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !ojson.ValidBytes(body) || !ojson.ParseBytes(body).IsObject() {
		return nil, errors.New("BPS OAuth access claims are invalid")
	}
	doc := ojson.NewDoc(body)
	if client := doc.Get("client_id").String(); client != "" && client != ClientID {
		return nil, errors.New("BPS OAuth access client mismatch")
	}
	email := doc.Get("email").String()
	if email == "" {
		email = doc.Get("https://api\\.openai\\.com/profile.email").String()
	}
	result := map[string]string{"email": email, "sub": doc.Get("sub").String(), "account_id": doc.Get("https://api\\.openai\\.com/auth.chatgpt_account_id").String()}
	if exp := doc.Get("exp").Int(); exp > 0 {
		result["expired"] = time.Unix(exp, 0).UTC().Format(time.RFC3339)
	}
	return result, nil
}

// Claims reads identity fields only; it does not establish token authenticity.
func Claims(token string) (map[string]string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("BPS OAuth identity token is invalid")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !ojson.ValidBytes(body) {
		return nil, errors.New("BPS OAuth identity claims are invalid")
	}
	doc := ojson.NewDoc(body)
	aud := doc.Get("aud")
	matched := aud.String() == ClientID
	if aud.IsArray() {
		for _, value := range aud.Array() {
			if value.String() == ClientID {
				matched = true
			}
		}
	}
	if !matched {
		return nil, errors.New("BPS OAuth identity client mismatch")
	}
	return map[string]string{"email": doc.Get("email").String(), "sub": doc.Get("sub").String(), "account_id": doc.Get("https://api\\.openai\\.com/auth.chatgpt_account_id").String()}, nil
}
