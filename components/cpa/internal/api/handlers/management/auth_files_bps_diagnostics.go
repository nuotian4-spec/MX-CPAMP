package management

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
)

type bpsOAuthDiagnostic struct {
	ErrorCode   string `json:"error_code"`
	Stage       string `json:"stage"`
	HTTPStatus  *int   `json:"http_status"`
	Reason      string `json:"reason"`
	SaveOutcome string `json:"save_outcome"`
}

func newBPSOAuthDiagnostic(code, reason string, status int) bpsOAuthDiagnostic {
	d := bpsOAuthDiagnostic{ErrorCode: code, Reason: "unknown", Stage: "unknown", SaveOutcome: "unknown"}
	switch code {
	case "oauth_exchange_failed":
		d.Stage, d.SaveOutcome = "token_exchange", "not_saved"
	case "oauth_identity_invalid", "oauth_target_identity_mismatch":
		d.Stage, d.SaveOutcome = "credential_validation", "not_saved"
	case "oauth_authorization_failed":
		d.Stage, d.SaveOutcome = "authorize", "not_saved"
	case "credential_commit_failed":
		d.Stage = "cpa_import"
	case "session_missing_or_expired":
		d.Stage = "session"
	default:
		d.ErrorCode = "unknown"
	}
	switch reason {
	case "invalid_grant", "invalid_client", "upstream_error", "transport_error", "protocol_error":
		d.Reason = reason
	}
	if status >= 100 && status <= 599 {
		d.HTTPStatus = &status
	}
	return d
}

func bpsExchangeDiagnostic(err error) bpsOAuthDiagnostic {
	reason, status := "unknown", 0
	var responseErr interface{ HTTPStatus() int }
	if errors.As(err, &responseErr) {
		status = responseErr.HTTPStatus()
	}
	var oauthErr *bps.OAuthError
	if errors.As(err, &oauthErr) && oauthErr != nil {
		reason = oauthErr.ResponseReason()
	} else if err != nil {
		switch err.Error() {
		case "BPS OAuth transport failed", "BPS OAuth response read failed", "BPS OAuth proxy configuration is invalid":
			reason = "transport_error"
		case "BPS OAuth code and PKCE are required", "BPS OAuth request is invalid",
			"BPS OAuth response is invalid", "BPS OAuth response has incomplete tokens", "BPS OAuth response has no refresh token",
			"BPS OAuth refreshed identity mismatch", "BPS OAuth access claims are invalid", "BPS OAuth access client mismatch",
			"BPS OAuth identity token is invalid", "BPS OAuth identity claims are invalid", "BPS OAuth identity client mismatch":
			reason = "protocol_error"
		}
	}
	return newBPSOAuthDiagnostic("oauth_exchange_failed", reason, status)
}

// setBPSError publishes only bounded diagnostic fields with the existing session TTL.
func (s *oauthSessionStore) setBPSError(state string, diagnostic bpsOAuthDiagnostic) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	session, ok := s.sessions[strings.TrimSpace(state)]
	if !ok || session.Provider != "bps" || session.Completed || (session.Committing && diagnostic.ErrorCode != "credential_commit_failed") {
		return
	}
	session.Status = diagnostic.ErrorCode
	session.BPSDiagnostic = &diagnostic
	session.ProxyURL = ""
	session.Committing = false
	session.SIWCCommitting = false
	session.ExpiresAt = now.Add(s.ttl)
	s.sessions[strings.TrimSpace(state)] = session
}

func writeBPSOAuthDiagnostic(c *gin.Context, status int, diagnostic bpsOAuthDiagnostic) {
	c.JSON(status, struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		bpsOAuthDiagnostic
	}{Status: "error", Error: diagnostic.ErrorCode, bpsOAuthDiagnostic: diagnostic})
}

// GetAuthStatusV8 adds BPS diagnostics without extending the legacy v0 contract.
func (h *Handler) GetAuthStatusV8(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	requestedBPS := strings.EqualFold(strings.TrimSpace(c.Query("provider")), "bps")
	if err := ValidateOAuthState(state); err != nil {
		if requestedBPS {
			if state == "" {
				writeBPSOAuthDiagnostic(c, http.StatusOK, newBPSOAuthDiagnostic("session_missing_or_expired", "unknown", 0))
			} else {
				writeBPSOAuthDiagnostic(c, http.StatusBadRequest, newBPSOAuthDiagnostic("unknown", "unknown", 0))
			}
			return
		}
		h.GetAuthStatus(c)
		return
	}
	session, ok := oauthSessions.Get(state)
	if !ok && requestedBPS {
		writeBPSOAuthDiagnostic(c, http.StatusOK, newBPSOAuthDiagnostic("session_missing_or_expired", "unknown", 0))
		return
	}
	if !ok || session.Provider != "bps" || session.Source != oauthSessionSourceBuiltin {
		h.GetAuthStatus(c)
		return
	}
	if session.Completed {
		h.writeCompletedOAuthStatus(c, state, session)
		return
	}
	if session.Status != "" {
		diagnostic := newBPSOAuthDiagnostic(session.Status, "unknown", 0)
		if session.BPSDiagnostic != nil {
			diagnostic = *session.BPSDiagnostic
		}
		writeBPSOAuthDiagnostic(c, http.StatusOK, diagnostic)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "wait"})
}
