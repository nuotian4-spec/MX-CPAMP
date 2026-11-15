package management

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	bpsauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) bpsResetTarget(c *gin.Context, allowRequestID bool) (*coreauth.Auth, string, string, bool) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	valid := err == nil
	for key, values := range query {
		if (key != "auth_index" && key != "auth_id" && !(allowRequestID && key == "redeem_request_id")) || len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			valid = false
		}
	}
	if !valid || query.Get("auth_index") == "" || query.Get("auth_id") == "" {
		c.JSON(400, gin.H{"error": "invalid_reference"})
		return nil, "", "", false
	}
	if h == nil || h.authManager == nil {
		c.JSON(503, gin.H{"error": "auth_manager_unavailable"})
		return nil, "", "", false
	}
	auth := h.authByIndex(query.Get("auth_index"))
	if auth == nil {
		c.JSON(404, gin.H{"error": "credential_not_found"})
		return nil, "", "", false
	}
	if auth.Provider != "bps" || auth.ID != query.Get("auth_id") {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return nil, "", "", false
	}
	account, sub := metadataStringValue(auth.Metadata, "account_id"), metadataStringValue(auth.Metadata, "sub")
	if account == "" || sub == "" || metadataStringValue(auth.Metadata, "access_token") == "" {
		c.JSON(400, gin.H{"error": "credential_incomplete"})
		return nil, "", "", false
	}
	cfg := h.configSnapshot()
	if cfg == nil || strings.TrimSpace(cfg.AuthDir) == "" {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return nil, "", "", false
	}
	dir, err := util.ResolveAuthDir(cfg.AuthDir)
	if err != nil || dir == "" {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return nil, "", "", false
	}
	identity := ojson.NewDocString("[]")
	if identity.Set("0", account) != nil || identity.Set("1", sub) != nil {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return nil, "", "", false
	}
	return auth, identity.String(), filepath.Join(dir, "bps-reset-ledger.brs"), true
}

// GetBPSCredentialQuota reads reset credits; account usage remains on its existing endpoint.
func (h *Handler) GetBPSCredentialQuota(c *gin.Context) {
	auth, account, path, ok := h.bpsResetTarget(c, false)
	if !ok {
		return
	}
	cfg := h.configSnapshot()
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	client := bpsauth.NewClient(bpsAccessProxy(auth, cfg))
	count, queryErr := client.ResetCreditsAvailable(c.Request.Context(), metadataStringValue(auth.Metadata, "access_token"), metadataStringValue(auth.Metadata, "account_id"))
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	bpsResetMu.Lock()
	ledger, err := loadBPSResetLedger(path)
	bpsResetMu.Unlock()
	if err != nil {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return
	}
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	var last *bpsResetReceipt
	lastID := ledger.Last[account]
	if record, found := ledger.Records[lastID]; found {
		receipt := record.Receipt
		last = &receipt
	}
	var available *int64
	var queryError *string
	if queryErr == nil {
		available = &count
	} else {
		code := bpsauth.ResetCreditErrorCode(queryErr)
		queryError = &code
	}
	c.JSON(200, gin.H{"provider": "bps", "auth_id": auth.ID, "auth_index": auth.EnsureIndex(), "source": "chatgpt_account_reset_credits", "available_count": available, "query_error": queryError, "supports_reset": true, "scope": "chatgpt_account", "queried_at": time.Now().UTC().Format(time.RFC3339), "last_reset_id": lastID, "last_reset": last})
}

func (h *Handler) GetBPSCredentialReset(c *gin.Context) {
	auth, account, path, ok := h.bpsResetTarget(c, true)
	if !ok {
		return
	}
	cfg := h.configSnapshot()
	id := c.Query("redeem_request_id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(400, gin.H{"error": "invalid_reset_reference"})
		return
	}
	bpsResetMu.Lock()
	ledger, err := loadBPSResetLedger(path)
	bpsResetMu.Unlock()
	if err != nil {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return
	}
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	record, found := ledger.Records[id]
	if !found || record.Account != account {
		c.JSON(404, gin.H{"error": "reset_not_found"})
		return
	}
	c.JSON(200, record.Receipt)
}

func (h *Handler) PostBPSCredentialReset(c *gin.Context) {
	auth, account, path, ok := h.bpsResetTarget(c, false)
	if !ok {
		return
	}
	raw, readErr := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	root := ojson.ParseBytes(raw)
	valid := readErr == nil && ojson.ValidBytes(raw) && root.IsObject() && root.Get("confirm").IsBool() && root.Get("confirm").Bool() && root.Get("redeem_request_id").Type == ojson.String && root.Get("previous_reset_id").Type == ojson.String
	for key := range root.Map() {
		if key != "confirm" && key != "redeem_request_id" && key != "previous_reset_id" {
			valid = false
		}
	}
	if !valid {
		c.JSON(400, gin.H{"error": "invalid_reset_confirmation"})
		return
	}
	previous := root.Get("previous_reset_id").String()
	input := struct {
		RequestID string
		Previous  *string
	}{root.Get("redeem_request_id").String(), &previous}
	if parsed, err := uuid.Parse(input.RequestID); err != nil || parsed == uuid.Nil || parsed.String() != input.RequestID {
		c.JSON(400, gin.H{"error": "invalid_reset_confirmation"})
		return
	}
	cfg := h.configSnapshot()
	bpsResetMu.Lock()
	defer bpsResetMu.Unlock()
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	ledger, err := loadBPSResetLedger(path)
	if err != nil {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return
	}
	if old, found := ledger.Records[input.RequestID]; found {
		if old.Account != account || old.AuthID != auth.ID || old.Previous != *input.Previous {
			c.JSON(409, gin.H{"error": "reset_request_conflict"})
			return
		}
		c.JSON(old.HTTPStatus, old.Receipt)
		return
	}
	lastID := ledger.Last[account]
	if lastID != *input.Previous {
		c.JSON(409, gin.H{"error": "reset_state_changed"})
		return
	}
	if last, found := ledger.Records[lastID]; found && (last.Receipt.Status == "pending" || last.Receipt.Status == "unknown") {
		c.JSON(409, gin.H{"error": "reset_result_unconfirmed"})
		return
	}
	client := bpsauth.NewClient(bpsAccessProxy(auth, cfg))
	token, accountID := metadataStringValue(auth.Metadata, "access_token"), metadataStringValue(auth.Metadata, "account_id")
	count, err := client.ResetCreditsAvailable(c.Request.Context(), token, accountID)
	if err != nil {
		c.JSON(502, gin.H{"error": bpsauth.ResetCreditErrorCode(err)})
		return
	}
	if count == 0 {
		c.JSON(409, gin.H{"error": "no_reset_cards"})
		return
	}
	if !h.sameBPSResetTarget(auth, cfg, path) {
		c.JSON(409, gin.H{"error": "credential_changed"})
		return
	}
	if c.Request.Context().Err() != nil {
		c.JSON(408, gin.H{"error": "request_canceled"})
		return
	}
	record := bpsResetRecord{Account: account, AuthID: auth.ID, Previous: *input.Previous, HTTPStatus: 409, Receipt: bpsResetReceipt{RequestID: input.RequestID, Status: "pending", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}}
	ledger.Records[input.RequestID] = record
	ledger.Last[account] = input.RequestID
	if err = saveBPSResetLedger(path, ledger); err != nil {
		c.JSON(503, gin.H{"error": "reset_state_unavailable"})
		return
	}
	err = client.ConsumeResetCredit(c.Request.Context(), token, accountID, input.RequestID)
	record.Receipt.Status = "submitted"
	record.HTTPStatus = 200
	if err != nil {
		record.Receipt.Status = "unknown"
		record.Receipt.Error = "reset_result_unknown"
		record.HTTPStatus = 502
	}
	record.Receipt.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	ledger.Records[input.RequestID] = record
	if saveBPSResetLedger(path, ledger) != nil {
		c.JSON(503, gin.H{"error": "reset_result_unconfirmed", "redeem_request_id": input.RequestID})
		return
	}
	c.JSON(record.HTTPStatus, record.Receipt)
}

func (h *Handler) sameBPSResetTarget(auth *coreauth.Auth, cfg *config.Config, path string) bool {
	current, currentCfg := h.authByIndex(auth.EnsureIndex()), h.configSnapshot()
	if !sameBPSAccessSnapshot(auth, current) || cfg == nil || currentCfg == nil || cfg.AuthDir != currentCfg.AuthDir || bpsAccessProxy(auth, cfg) != bpsAccessProxy(current, currentCfg) {
		return false
	}
	dir, err := util.ResolveAuthDir(currentCfg.AuthDir)
	return err == nil && filepath.Join(dir, "bps-reset-ledger.brs") == path
}
