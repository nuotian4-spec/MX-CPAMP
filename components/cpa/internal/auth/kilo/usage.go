package kilo

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
)

const UsageURL = "https://chatgpt.com/backend-api/wham/usage"
const UsageMaxBytes = 128 * 1024

type UsageError string

func (e UsageError) Error() string { return string(e) }

type UsageWindow struct {
	Group            string     `json:"group"`
	Slot             string     `json:"slot"`
	DurationSeconds  *float64   `json:"duration_seconds"`
	RemainingPercent *float64   `json:"remaining_percent"`
	UsedPercent      *float64   `json:"used_percent"`
	ResetAt          *time.Time `json:"reset_at"`
	Source           string     `json:"source"`
}
type UsageCredits struct {
	Remaining   *float64 `json:"remaining"`
	SourceField *string  `json:"source_field"`
	Unit        *string  `json:"unit"`
}
type UsageObservation struct {
	MainWindowsInvalid bool          `json:"-"`
	Plan               *string       `json:"plan"`
	Windows            []UsageWindow `json:"windows"`
	Credits            UsageCredits  `json:"credits"`
}

// FetchUsage is a single bounded management GET; it never follows redirects or retries.
func FetchUsage(ctx context.Context, client *http.Client, token, account string) (UsageObservation, error) {
	return fetchUsageBudget(ctx, client, token, account, 5*time.Second)
}
func fetchUsageBudget(parent context.Context, client *http.Client, token, account string, budget time.Duration) (UsageObservation, error) {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UsageURL, nil)
	if err != nil {
		return UsageObservation{}, UsageError("usage_invalid_response")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if account != "" {
		req.Header.Set("ChatGPT-Account-Id", account)
	}
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return UsageObservation{}, UsageError("usage_cancelled")
		}
		return UsageObservation{}, UsageError("usage_network_error")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, UsageMaxBytes+1))
	if parent.Err() != nil {
		return UsageObservation{}, UsageError("usage_cancelled")
	}
	if err != nil || ctx.Err() != nil {
		return UsageObservation{}, UsageError("usage_network_error")
	}
	switch resp.StatusCode {
	case 200:
	case 401:
		return UsageObservation{}, UsageError("usage_unauthorized")
	case 403:
		return UsageObservation{}, UsageError("usage_forbidden")
	case 429:
		return UsageObservation{}, UsageError("usage_rate_limited")
	default:
		return UsageObservation{}, UsageError("usage_http_error")
	}
	if len(body) > UsageMaxBytes {
		return UsageObservation{}, UsageError("usage_invalid_response")
	}
	return ParseUsage(body, time.Now())
}

func usageNumber(v ojson.Result) *float64 {
	if v.Type != ojson.Number && v.Type != ojson.String {
		return nil
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(v.String()), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return nil
	}
	return &n
}
func usageReset(w ojson.Result, now time.Time) *time.Time {
	raw := w.Get("reset_at")
	n := usageNumber(raw)
	if n == nil && raw.Type == ojson.String {
		if date, err := time.Parse(time.RFC3339, raw.String()); err == nil {
			seconds := float64(date.Unix())
			n = &seconds
		}
	}
	if n != nil && *n > 1e12 {
		value := *n / 1000
		n = &value
	}
	if n == nil || *n <= 0 || *n > 253402300799 {
		n = nil
		if after := usageNumber(w.Get("reset_after_seconds")); after != nil && *after >= 0 && *after <= 253402300799-float64(now.Unix()) {
			value := float64(now.Unix()) + *after
			n = &value
		}
	}
	if n == nil {
		return nil
	}
	t := time.Unix(int64(*n), 0).UTC()
	if !t.After(now) {
		return nil
	}
	return &t
}

// ParseUsage projects only recognized observation fields, preserving unknown numeric values.
func ParseUsage(body []byte, now time.Time) (UsageObservation, error) {
	root := ojson.ParseBytes(body)
	if !ojson.ValidBytes(body) || !root.IsObject() {
		return UsageObservation{}, UsageError("usage_invalid_response")
	}
	known := false
	for _, key := range []string{"rate_limit", "additional_rate_limits", "credits", "spend_control", "plan_type"} {
		known = known || root.Get(key).Exists()
	}
	if !known {
		return UsageObservation{}, UsageError("usage_invalid_response")
	}
	out := UsageObservation{Windows: []UsageWindow{}}
	if plan := root.Get("plan_type"); plan.Type == ojson.String {
		v := []rune(plan.String())
		if len(v) > 100 {
			v = v[:100]
		}
		s := string(v)
		out.Plan = &s
	}
	add := func(name string, group ojson.Result) {
		for _, slot := range []string{"primary_window", "secondary_window"} {
			w := group.Get(slot)
			if !w.IsObject() || w.Raw == "{}" {
				if name == "rate_limit" && w.Exists() && w.Type != ojson.Null {
					out.MainWindowsInvalid = true
				}
				continue
			}
			item := UsageWindow{Group: name, Slot: slot, Source: "api_query", ResetAt: usageReset(w, now)}
			if n := usageNumber(w.Get("limit_window_seconds")); n != nil && *n > 0 && *n <= 315360000 {
				item.DurationSeconds = n
			}
			if used := usageNumber(w.Get("used_percent")); used != nil && *used >= 0 {
				item.UsedPercent = used
				value := math.Round(math.Max(0, 100-*used)*100) / 100
				item.RemainingPercent = &value
			}
			out.Windows = append(out.Windows, item)
		}
	}
	add("rate_limit", root.Get("rate_limit"))
	for i, item := range root.Get("additional_rate_limits").Array() {
		add("additional_rate_limits:"+strconv.Itoa(i), item.Get("rate_limit"))
	}
	field := "spend_control.individual_limit.remaining"
	n := usageNumber(root.Get(field))
	if n == nil {
		limit, used := usageNumber(root.Get("spend_control.individual_limit.limit")), usageNumber(root.Get("spend_control.individual_limit.used"))
		if limit != nil && used != nil {
			value := *limit - *used
			if !math.IsInf(value, 0) {
				n = &value
				field = "spend_control.individual_limit.limit-used"
			}
		}
	}
	for _, candidate := range []string{"credits.remaining", "credits.balance"} {
		if n == nil {
			n = usageNumber(root.Get(candidate))
			field = candidate
		}
	}
	if n != nil {
		value := math.Max(0, *n)
		out.Credits.Remaining = &value
		out.Credits.SourceField = &field
	}
	// The source protocol does not establish a currency or unit for credits.
	return out, nil
}

// UsageRecoveryReset recognizes only the source protocol's supported primary-window combinations.
func UsageRecoveryReset(observation UsageObservation, now time.Time) *time.Time {
	windows := map[float64]UsageWindow{}
	for _, w := range observation.Windows {
		if w.Group == "rate_limit" && w.DurationSeconds != nil {
			windows[*w.DurationSeconds] = w
		}
	}
	var chosen UsageWindow
	if len(windows) == 2 {
		short, shortOK := windows[18000]
		week, weekOK := windows[604800]
		if !shortOK || !weekOK || week.RemainingPercent == nil {
			return nil
		}
		chosen = short
		if *week.RemainingPercent == 0 {
			chosen = week
		}
	} else if len(windows) == 1 {
		var ok bool
		chosen, ok = windows[604800]
		if !ok {
			chosen, ok = windows[2592000]
		}
		if !ok {
			return nil
		}
	} else {
		return nil
	}
	if chosen.ResetAt == nil || !chosen.ResetAt.After(now) {
		return nil
	}
	return chosen.ResetAt
}

// KnownAccessExpired is only an expiry hint; unverified JWT identity is never used for binding.
func KnownAccessExpired(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	n := usageNumber(ojson.GetBytes(body, "exp"))
	return n != nil && *n > 0 && *n <= float64(now.Unix())
}
func UsageErrorCode(err error) string {
	var code UsageError
	if errors.As(err, &code) {
		return string(code)
	}
	return "usage_network_error"
}
