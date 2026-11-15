package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	device "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func kiloDeviceError(c *gin.Context, err error) {
	status, message := 503, "Kilo authorization operation failed"
	var upstream *device.DeviceError
	if errors.As(err, &upstream) {
		status, message = upstream.Status, upstream.Message
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status, message = 499, "Kilo authorization request canceled"
	}
	c.JSON(status, gin.H{"error": message})
}
func kiloDeviceBody(c *gin.Context, empty bool) (ojson.Result, bool) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 65537))
	if empty && len(strings.TrimSpace(string(raw))) == 0 && err == nil {
		raw = []byte("{}")
	}
	root := ojson.ParseBytes(raw)
	valid := err == nil && len(raw) <= 65536 && ojson.ValidBytes(raw) && root.IsObject()
	if empty {
		root.ForEach(func(_, _ ojson.Result) bool { valid = false; return false })
	}
	if !valid {
		c.JSON(400, gin.H{"error": "invalid Kilo authorization fields"})
	}
	return root, valid
}
func (h *Handler) StartKiloDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	root, ok := kiloDeviceBody(c, false)
	if !ok {
		return
	}
	fields := map[string]ojson.Result{}
	root.ForEach(func(key, value ojson.Result) bool { fields[key.String()] = value; return true })
	authorization, authorizationErr := kilo.DecodeAuthorizationEgress(fields["authorization_egress"])
	inference, inferenceErr := kilo.DecodeInferenceEgress(fields["inference_egress"])
	mode := authorization.Mode
	valid := authorizationErr == nil && inferenceErr == nil
	if fields["inference_egress"].Exists() && inference.ExpectedRevision == nil {
		valid = false
	}
	doc := ojson.NewDocString("{}")
	for key, value := range fields {
		if key == "authorization_egress" || key == "inference_egress" {
			continue
		}
		if key == "api_key" || key == "organization_id" {
			valid = false
			continue
		}
		_ = doc.SetRaw(key, value.Raw)
	}
	settings, err := decodeKiloAccountFields(ojson.ParseBytes(doc.Bytes()))
	if settings.Name == nil {
		valid = false
	} else {
		name := strings.TrimSpace(*settings.Name)
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 256 {
			valid = false
		}
		settings.Name = &name
	}
	if !valid || err != nil {
		c.JSON(400, gin.H{"error": "invalid Kilo authorization fields"})
		return
	}
	source, err := h.kiloStateSource()
	if err != nil {
		kiloDeviceError(c, err)
		return
	}
	oauthSessions.purgeKilo()
	id := uuid.NewString()
	accountID := "kilo-" + uuid.NewString() + ".json"
	ctx, cancel := context.WithCancel(context.Background())
	authorization, inference, err = source.PrepareKiloDevice("device-"+id, accountID, authorization, inference, cancel)
	if err != nil {
		cancel()
		kiloProxyError(c, err)
		return
	}
	requestCtx, requestCancel := context.WithCancel(c.Request.Context())
	stop := context.AfterFunc(ctx, requestCancel)
	result, err := source.StartKiloDevice(requestCtx, "device-"+id, mode)
	stop()
	requestCancel()
	if err != nil {
		source.ReleaseKiloDevice("device-" + id)
		cancel()
		kiloDeviceError(c, err)
		return
	}
	flow := &kiloDeviceSession{owner: h, ctx: ctx, cancel: cancel, state: "pending", accountID: accountID, mode: mode, code: result.Code, settings: settings, expires: oauthSessions.kiloNow().Add(time.Duration(result.ExpiresIn) * time.Second), authorization: authorization, inference: inference, releasePin: func() { source.ReleaseKiloDevice("device-" + id) }}
	flow.armExpiryLocked(oauthSessions.kiloNow(), oauthSessions.kiloNow)
	oauthSessions.mu.Lock()
	oauthSessions.sessions[id] = oauthSession{Provider: "kilo", Status: "kilo-device", Kilo: flow}
	oauthSessions.mu.Unlock()
	c.JSON(200, gin.H{"id": id, "user_code": result.UserCode, "verification_url": result.VerificationURL, "expires_in": result.ExpiresIn, "interval": 3, "inference_egress_mode": inference.Mode, "authorization_egress": authorization, "inference_egress": inference})
}
func (h *Handler) PollKiloDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := kiloDeviceBody(c, true); !ok {
		return
	}
	flow := oauthSessions.kiloSession(c.Param("id"), h)
	if flow == nil {
		c.JSON(404, gin.H{"error": "Kilo authorization session not found"})
		return
	}
	flow.mu.Lock()
	now := oauthSessions.kiloNow()
	flow.expireLocked(now)
	if flow.state != "pending" || flow.polling || now.Before(flow.nextPoll) {
		view := flow.viewLocked(now)
		flow.mu.Unlock()
		c.JSON(200, view)
		return
	}
	flow.polling, flow.nextPoll = true, now.Add(3*time.Second)
	code, mode := flow.code, flow.mode
	ctx, cancel := context.WithTimeout(c.Request.Context(), max(time.Nanosecond, flow.expires.Sub(now)))
	stop := context.AfterFunc(flow.ctx, cancel)
	flow.mu.Unlock()
	source, err := h.kiloStateSource()
	var approval device.DeviceApproval
	if err == nil {
		approval, err = source.PollKiloDevice(ctx, "device-"+c.Param("id"), mode, code)
	}
	stop()
	cancel()
	flow.mu.Lock()
	now = oauthSessions.kiloNow()
	flow.polling = false
	flow.expireLocked(now)
	if flow.state != "pending" {
		view := flow.viewLocked(now)
		flow.mu.Unlock()
		c.JSON(200, view)
		return
	}
	if err != nil {
		flow.mu.Unlock()
		kiloDeviceError(c, err)
		return
	}
	switch approval.Status {
	case "approved":
		flow.state, flow.token, flow.email, flow.code = "approved", approval.Token, approval.Email, ""
		flow.expires = now.Add(300 * time.Second)
		flow.armExpiryLocked(now, oauthSessions.kiloNow)
	case "denied", "expired":
		flow.terminalLocked(approval.Status, now)
	}
	view := flow.viewLocked(now)
	flow.mu.Unlock()
	c.JSON(200, view)
}
func (h *Handler) SaveKiloDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := kiloDeviceBody(c, true); !ok {
		return
	}
	flow := oauthSessions.kiloSession(c.Param("id"), h)
	if flow == nil {
		c.JSON(404, gin.H{"error": "Kilo authorization session not found"})
		return
	}
	flow.mu.Lock()
	flow.expireLocked(oauthSessions.kiloNow())
	if flow.committing || (flow.state != "approved" && flow.state != "saved") {
		flow.mu.Unlock()
		c.JSON(409, gin.H{"error": "Kilo authorization is not ready to save"})
		return
	}
	flow.committing = true
	saved, id, email, settings, token := flow.state == "saved", flow.accountID, flow.email, flow.settings, flow.token
	flow.mu.Unlock()
	var account *coreauth.Auth
	var err error
	if saved {
		account, _ = h.authManager.GetByID(id)
		if account == nil {
			err = coreauth.ErrKiloAccountMissing
		}
	} else {
		settings.APIKey = &token
		account, err = h.authManager.SaveKiloDeviceAccountWithEgress(c.Request.Context(), id, settings, email, "device-"+c.Param("id"), flow.inference)
	}
	flow.mu.Lock()
	if err == nil {
		flow.terminalLocked("saved", oauthSessions.kiloNow())
	}
	flow.committing = false
	flow.expireLocked(oauthSessions.kiloNow())
	flow.mu.Unlock()
	if err != nil {
		kiloDeviceError(c, err)
		return
	}
	if h.postAuthPersistHook != nil {
		if err = h.postAuthPersistHook(c.Request.Context(), account); err != nil {
			kiloDeviceError(c, err)
			return
		}
	}
	entry := coreauth.CredentialConcurrencyEntry{EffectiveLimit: coreauth.DefaultCredentialConcurrency}
	entries, _ := h.authManager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry)
	for _, item := range entries {
		if item.AuthID == account.ID {
			entry = item
			break
		}
	}
	status, reason := "pending", "shared_proxy_not_configured"
	if source, sourceErr := h.kiloStateSource(); sourceErr == nil {
		status, reason = source.KiloDeviceSaveEgress(account.ID)
	}
	var egressError *string
	if reason != "" {
		egressError = &reason
	}
	c.JSON(http.StatusCreated, struct {
		kiloAccountDTO
		CredentialSaved bool                 `json:"credential_saved"`
		EgressReady     bool                 `json:"egress_ready"`
		EgressError     *string              `json:"egress_error"`
		EgressMode      string               `json:"egress_mode"`
		EgressStatus    string               `json:"egress_status"`
		EgressReason    string               `json:"egress_reason"`
		InferenceEgress kilo.InferenceEgress `json:"inference_egress"`
	}{h.kiloAccountView(account, entry), true, status == "available", egressError, flow.inference.Mode, status, reason, flow.inference})
}
func (h *Handler) CancelKiloDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := kiloDeviceBody(c, true); !ok {
		return
	}
	flow := oauthSessions.kiloSession(c.Param("id"), h)
	if flow == nil {
		c.JSON(200, gin.H{"status": "cancelled"})
		return
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.committing {
		c.JSON(409, gin.H{"error": "Kilo authorization save is in progress"})
		return
	}
	now := oauthSessions.kiloNow()
	if flow.state != "saved" {
		flow.terminalLocked("cancelled", now)
	}
	c.JSON(200, flow.viewLocked(now))
}
