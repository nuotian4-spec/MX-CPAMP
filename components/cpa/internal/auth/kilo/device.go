package kilo

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
)

const DeviceCodesURL = "https://api.kilo.ai/api/device-auth/codes"

// DeviceError carries only a fixed public message, never an upstream response.
type DeviceError struct {
	Status  int
	Message string
}

func (e *DeviceError) Error() string { return e.Message }
func DeviceUnavailable() error       { return &DeviceError{503, "Kilo authorization egress is unavailable"} }
func invalidDeviceResponse() error {
	return &DeviceError{502, "Kilo authorization response is invalid"}
}

type DeviceCode struct {
	Code, UserCode, VerificationURL string
	ExpiresIn                       int
}
type DeviceApproval struct{ Status, Token, Email string }

func ValidVerificationURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "kilo.ai" || strings.HasSuffix(host, ".kilo.ai")
}
func validDeviceToken(token string) bool {
	if token == "" {
		return false
	}
	for _, b := range []byte(token) {
		if b <= 32 || b >= 127 {
			return false
		}
	}
	return true
}
func deviceHTTP(ctx context.Context, client *http.Client, method, endpoint string) (int, []byte, error) {
	return deviceHTTPBudget(ctx, client, method, endpoint, 30*time.Second)
}
func deviceHTTPBudget(parent context.Context, client *http.Client, method, endpoint string, budget time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return 0, nil, invalidDeviceResponse()
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return 0, nil, parent.Err()
		}
		return 0, nil, &DeviceError{503, "Kilo authorization connection failed"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if parent.Err() != nil {
		return 0, nil, parent.Err()
	}
	if err != nil || ctx.Err() != nil {
		return 0, nil, &DeviceError{503, "Kilo authorization connection failed"}
	}
	if len(body) > 65536 {
		return 0, nil, invalidDeviceResponse()
	}
	return response.StatusCode, body, nil
}
func StartDevice(ctx context.Context, client *http.Client) (DeviceCode, error) {
	status, body, err := deviceHTTP(ctx, client, http.MethodPost, DeviceCodesURL)
	if err != nil {
		return DeviceCode{}, err
	}
	if status == 429 {
		return DeviceCode{}, &DeviceError{429, "Kilo authorization is rate limited"}
	}
	if status < 200 || status >= 300 || !ojson.ValidBytes(body) {
		return DeviceCode{}, invalidDeviceResponse()
	}
	root := ojson.ParseBytes(body)
	if !root.IsObject() {
		return DeviceCode{}, invalidDeviceResponse()
	}
	code, user, target, expires := root.Get("code"), root.Get("user_code"), root.Get("verificationUrl"), root.Get("expiresIn")
	seconds, parseErr := strconv.ParseInt(expires.Raw, 10, 32)
	if code.Type != ojson.String || strings.TrimSpace(code.String()) == "" || user.Type != ojson.String || strings.TrimSpace(user.String()) == "" || target.Type != ojson.String || !ValidVerificationURL(target.String()) || expires.Type != ojson.Number || parseErr != nil || seconds < 1 || seconds > 86400 {
		return DeviceCode{}, invalidDeviceResponse()
	}
	return DeviceCode{code.String(), user.String(), target.String(), int(seconds)}, nil
}
func PollDevice(ctx context.Context, client *http.Client, code string) (DeviceApproval, error) {
	status, body, err := deviceHTTP(ctx, client, http.MethodGet, DeviceCodesURL+"/"+url.PathEscape(code))
	if err != nil {
		return DeviceApproval{}, err
	}
	switch status {
	case 202:
		return DeviceApproval{Status: "pending"}, nil
	case 403:
		return DeviceApproval{Status: "denied"}, nil
	case 410:
		return DeviceApproval{Status: "expired"}, nil
	case 429:
		return DeviceApproval{}, &DeviceError{429, "Kilo authorization is rate limited"}
	}
	if status < 200 || status >= 300 || !ojson.ValidBytes(body) {
		return DeviceApproval{}, invalidDeviceResponse()
	}
	root := ojson.ParseBytes(body)
	token, email := root.Get("token"), root.Get("userEmail")
	if !root.IsObject() || token.Type != ojson.String || !validDeviceToken(token.String()) || email.Type != ojson.String {
		return DeviceApproval{}, invalidDeviceResponse()
	}
	return DeviceApproval{Status: "approved", Token: token.String(), Email: email.String()}, nil
}
