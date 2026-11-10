package oauthlogin

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
