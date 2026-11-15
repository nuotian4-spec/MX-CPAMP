package oauthlogin

// HTTPDiagnostics contains only bounded observations, never request material.
type HTTPDiagnostics struct {
	HTTPStatus             *int   `json:"http_status"`
	EndpointCategory       string `json:"endpoint_category"`
	SecurityCheckSubtype   string `json:"security_check_subtype"`
	ChallengeHeaderPresent *bool  `json:"challenge_header_present"`
}

type rawHTTPDiagnostics struct {
	HTTPStatus             any `json:"http_status"`
	EndpointCategory       any `json:"endpoint_category"`
	SecurityCheckSubtype   any `json:"security_check_subtype"`
	ChallengeHeaderPresent any `json:"challenge_header_present"`
}

func safeHTTPStatus(value any) *int {
	n, ok := value.(float64)
	if !ok || n < 100 || n > 599 || n != float64(int(n)) {
		return nil
	}
	status := int(n)
	return &status
}

func (raw rawHTTPDiagnostics) safe() HTTPDiagnostics {
	out := HTTPDiagnostics{HTTPStatus: safeHTTPStatus(raw.HTTPStatus), EndpointCategory: "unknown", SecurityCheckSubtype: "unknown"}
	if value, ok := raw.EndpointCategory.(string); ok {
		switch value {
		case "chatgpt_home", "chatgpt_providers", "chatgpt_csrf", "chatgpt_signin", "auth_redirect", "password_verify", "totp_issue", "totp_verify", "workspace_select", "session_select", "web_finalize", "oauth_authorize", "oauth_token", "sentinel":
			out.EndpointCategory = value
		}
	}
	if value, ok := raw.SecurityCheckSubtype.(string); ok {
		switch value {
		case "challenge_header", "challenge_html", "html_forbidden_unclassified":
			out.SecurityCheckSubtype = value
		}
	}
	if value, ok := raw.ChallengeHeaderPresent.(bool); ok {
		out.ChallengeHeaderPresent = &value
	}
	return out
}

func safeStage(value string) string {
	switch value {
	case "input", "web_login", "authorize", "email", "email_otp", "password", "totp", "workspace_session", "callback", "token_exchange", "credential_validation", "cpa_import":
		return value
	}
	return "unknown"
}

func safeReason(value string) string {
	switch value {
	case "security_check", "email_verification_required", "phone_verification_required", "account_setup_required", "unexpected_login_page", "session_selection_required", "totp_factor_missing", "interaction_required", "password_rejected", "totp_rejected", "callback_invalid", "token_exchange_failed", "credentials_incomplete", "identity_mismatch", "invalid_profile", "transport_error", "protocol_error":
		return value
	}
	return "unknown"
}
