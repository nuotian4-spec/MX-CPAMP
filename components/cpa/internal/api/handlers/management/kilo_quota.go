package management

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func kiloQuotaError(c *gin.Context, err error) {
	status, code := 503, "quota_unavailable"
	switch {
	case errors.Is(err, coreauth.ErrKiloQuotaInvalid):
		status, code = 400, "quota_invalid_request"
	case errors.Is(err, coreauth.ErrKiloQuotaConflict):
		status, code = 409, "quota_conflict"
	case errors.Is(err, coreauth.ErrKiloAccountMissing):
		status, code = 404, "account_missing"
	case errors.Is(err, coreauth.ErrKiloAccountStorage):
		status, code = 503, "quota_storage_unavailable"
	}
	c.JSON(status, gin.H{"error": code})
}
func (h *Handler) GetKiloQuotaSources(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	c.JSON(200, gin.H{"sources": h.authManager.KiloQuotaSources()})
}
func (h *Handler) GetKiloQuota(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	view, err := h.authManager.KiloQuota(c.Param("id"))
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	c.JSON(200, view)
}
func decodeKiloQuota(c *gin.Context, put bool) (uint64, *coreauth.KiloQuotaBinding, error) {
	invalid := coreauth.ErrKiloQuotaInvalid
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 128*1024+1))
	if err != nil || len(body) > 128*1024 || !ojson.ValidBytes(body) {
		return 0, nil, invalid
	}
	root := ojson.ParseBytes(body)
	if !root.IsObject() {
		return 0, nil, invalid
	}
	fields := map[string]ojson.Result{}
	root.ForEach(func(k, v ojson.Result) bool { fields[k.String()] = v; return true })
	for key := range fields {
		if key != "expected_binding_revision" && (!put || key != "source") {
			return 0, nil, invalid
		}
	}
	rev := fields["expected_binding_revision"]
	if rev.Type != ojson.Number {
		return 0, nil, invalid
	}
	revision, err := strconv.ParseUint(rev.Raw, 10, 53)
	if err != nil {
		return 0, nil, invalid
	}
	if !put {
		return revision, nil, nil
	}
	source := fields["source"]
	if !source.IsObject() {
		return 0, nil, invalid
	}
	values := map[string]string{}
	valid := true
	source.ForEach(func(k, v ojson.Result) bool {
		if v.Type != ojson.String {
			valid = false
		}
		values[k.String()] = v.String()
		return true
	})
	if !valid {
		return 0, nil, invalid
	}
	kind := values["kind"]
	allowed := map[string]bool{"kind": true, "chatgpt_account_id": true}
	switch kind {
	case "cpa_codex":
		allowed["credential_id"] = true
	case "imported":
		allowed["access_token"] = true
	default:
		return 0, nil, invalid
	}
	for key, value := range values {
		if !allowed[key] || strings.ContainsAny(value, "\r\n") {
			return 0, nil, invalid
		}
	}
	b := &coreauth.KiloQuotaBinding{Kind: kind, CredentialID: values["credential_id"], AccountID: values["chatgpt_account_id"], AccessToken: values["access_token"]}
	if kind == "cpa_codex" && (strings.TrimSpace(b.CredentialID) == "" || strings.TrimSpace(b.AccountID) == "") {
		return 0, nil, invalid
	}
	if kind == "imported" && strings.TrimSpace(b.AccessToken) == "" {
		return 0, nil, invalid
	}
	return revision, b, nil
}
func (h *Handler) PutKiloQuotaSource(c *gin.Context)    { h.changeKiloQuotaSource(c, true) }
func (h *Handler) DeleteKiloQuotaSource(c *gin.Context) { h.changeKiloQuotaSource(c, false) }
func (h *Handler) changeKiloQuotaSource(c *gin.Context, put bool) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	revision, binding, err := decodeKiloQuota(c, put)
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	if !put {
		binding = &coreauth.KiloQuotaBinding{Kind: "none"}
	}
	_, err = h.authManager.ChangeKiloAccount(c.Request.Context(), c.Param("id"), coreauth.KiloAccountChanges{QuotaSource: binding, ExpectedBindingRevision: &revision}, false, false)
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	view, err := h.authManager.KiloQuota(c.Param("id"))
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	c.JSON(200, view)
}
func (h *Handler) QueryKiloQuota(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authManager == nil {
		kiloQuotaError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	revision, _, err := decodeKiloQuota(c, false)
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	if _, err = h.kiloStateSource(); err != nil {
		kiloQuotaError(c, err)
		return
	}
	view, err := h.authManager.QueryKiloQuota(c.Request.Context(), c.Param("id"), revision)
	if err != nil {
		kiloQuotaError(c, err)
		return
	}
	status := 200
	if view.Query.Status == "cancelled" {
		status = 499
	}
	c.JSON(status, view)
}
