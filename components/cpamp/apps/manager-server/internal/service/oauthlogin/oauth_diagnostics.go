package oauthlogin

import "errors"

func validSaveOutcome(value string) bool {
	return value == "unknown" || value == "not_saved" || value == "not_submitted" || value == "confirmed"
}

func oauthOutcome(raw map[string]any, submitted bool) map[string]any {
	code, _ := raw["error_code"].(string)
	if code == "" {
		code, _ = raw["error"].(string)
	}
	stage, outcome, source := "unknown", "unknown", "upstream"
	switch code {
	case "unknown or expired state", "session_missing_or_expired":
		code, stage = "session_missing_or_expired", "session"
		source = "cpa_session"
	case "oauth_authorization_failed":
		stage, outcome = "authorize", "not_saved"
	case "oauth_exchange_failed":
		stage, outcome = "token_exchange", "not_saved"
	case "oauth_identity_invalid", "oauth_target_identity_mismatch":
		stage, outcome = "credential_validation", "not_saved"
	case "credential_commit_failed":
		stage = "cpa_import"
	default:
		code = "unknown"
	}
	if !submitted {
		outcome = "not_submitted"
	} else if actual, ok := raw["save_outcome"].(string); ok && validSaveOutcome(actual) {
		outcome = actual
	}
	reason, _ := raw["reason"].(string)
	switch reason {
	case "invalid_grant", "invalid_client", "upstream_error", "transport_error", "protocol_error":
	default:
		reason = "unknown"
	}
	return map[string]any{"provider": "bps", "error_code": code, "error_source": source, "stage": stage, "reason": reason, "http_status": safeHTTPStatus(raw["http_status"]), "save_outcome": outcome}
}

// OAuthRequestDiagnostic preserves only safe request failure context.
func OAuthRequestDiagnostic(action string, err error) map[string]any {
	stage, code, outcome, source := "unknown", "unknown", "unknown", "manager_session"
	switch action {
	case "auth-url":
		stage, code, outcome = "authorize", "oauth_start_failed", "not_submitted"
	case "status", "session":
		stage, code = "session", "oauth_status_unavailable"
	case "callback":
		stage, code = "callback", "callback_result_unknown"
		if rejected := CallbackRejectionCode(err); rejected != "" {
			code, outcome = rejected, "not_saved"
		}
		if errors.Is(err, ErrInput) {
			code, outcome = "invalid_callback_input", "not_saved"
		}
	}
	if errors.Is(err, ErrNotFound) {
		stage, code = "session", "session_missing_or_expired"
	}
	if errors.Is(err, ErrImporting) {
		stage, code = "cpa_import", "oauth_commit_in_progress"
	}
	var failure *requestFailure
	if errors.As(err, &failure) {
		source = failure.source
	}
	var upstream *upstreamError
	var status *int
	if errors.As(err, &upstream) && upstream.status >= 100 && upstream.status <= 599 {
		source = "upstream"
		value := upstream.status
		status = &value
	}
	return map[string]any{"error_code": code, "error_source": source, "stage": stage, "http_status": status, "save_outcome": outcome}
}

// copyOAuthOutcome retains only the sanitized result within the existing session TTL.
func copyOAuthOutcome(out map[string]any) map[string]any {
	if out == nil {
		return nil
	}
	copied := make(map[string]any, len(out))
	for key, value := range out {
		copied[key] = value
	}
	return copied
}
