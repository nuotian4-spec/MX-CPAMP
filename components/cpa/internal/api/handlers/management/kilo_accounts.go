package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	kiloprofile "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func decodeKiloAccountChanges(c *gin.Context) (coreauth.KiloAccountChanges, error) {
	var change coreauth.KiloAccountChanges
	invalid := errors.New("invalid Kilo account fields")
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !ojson.ValidBytes(raw) {
		return change, invalid
	}
	root := ojson.ParseBytes(raw)
	if !root.IsObject() {
		return change, invalid
	}
	return decodeKiloAccountFields(root)
}
func decodeKiloAccountFields(root ojson.Result) (coreauth.KiloAccountChanges, error) {
	var change coreauth.KiloAccountChanges
	invalid := errors.New("invalid Kilo account fields")
	// Preserve the request contract's last-value semantics for duplicate fields.
	fields := map[string]ojson.Result{}
	root.ForEach(func(key, value ojson.Result) bool { fields[key.String()] = value; return true })
	for name, value := range fields {
		if value.Type == ojson.Null && name != "concurrency_limit" {
			return change, invalid
		}
		switch name {
		case "organization_id":
			if value.Type != ojson.String || (value.String() != "" && !kiloprofile.ValidOrganizationID(value.String())) {
				return change, invalid
			}
			id := value.String()
			change.OrganizationID = &id
		case "api_key", "name":
			if value.Type != ojson.String {
				return change, invalid
			}
			text := value.String()
			if name == "api_key" {
				if strings.TrimSpace(text) == "" {
					return change, invalid
				}
				change.APIKey = &text
			} else {
				change.Name = &text
			}
		case "priority", "concurrency_limit":
			if name == "concurrency_limit" {
				change.ConcurrencySet = true
				if value.Type == ojson.Null {
					continue
				}
			}
			if value.Type != ojson.Number {
				return change, invalid
			}
			integer, err := strconv.ParseInt(value.Raw, 10, strconv.IntSize)
			if err != nil {
				return change, invalid
			}
			number := int(integer)
			if name == "priority" {
				change.Priority = &number
			} else {
				if _, err = coreauth.ParseConcurrencyLimit(number); err != nil {
					return change, invalid
				}
				change.ConcurrencyLimit = &number
			}
		case "enabled", "allow_direct_when_proxy_full":
			if !value.IsBool() {
				return change, invalid
			}
			enabled := value.Bool()
			if name == "enabled" {
				change.Enabled = &enabled
			} else {
				change.AllowDirectWhenProxyFull = &enabled
			}
		default:
			return change, errors.New("unsupported Kilo account field")
		}
	}
	return change, nil
}
func kiloAccountError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "Kilo account operation failed"
	var profileError *kiloprofile.ProfileError
	switch {
	case errors.As(err, &profileError):
		status, message = profileError.Status, profileError.Message
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = 499, "Kilo account operation canceled"
	case errors.Is(err, coreauth.ErrKiloOrganizationDenied):
		status, message = http.StatusForbidden, err.Error()
	case errors.Is(err, coreauth.ErrKiloIdentityChanged):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, coreauth.ErrKiloAccountMissing):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, coreauth.ErrKiloAccountBusy):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, coreauth.ErrKiloAccountStorage):
		status, message = http.StatusServiceUnavailable, err.Error()
	}
	c.JSON(status, gin.H{"error": message})
}
func (h *Handler) CreateKiloAccount(c *gin.Context) { h.changeKiloAccount(c, true) }
func (h *Handler) PatchKiloAccount(c *gin.Context)  { h.changeKiloAccount(c, false) }
func (h *Handler) changeKiloAccount(c *gin.Context, create bool) {
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	change, err := decodeKiloAccountChanges(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if create && change.APIKey == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key is required"})
		return
	}
	if create && change.OrganizationID != nil {
		c.JSON(400, gin.H{"error": "organization_id requires an existing Kilo account"})
		return
	}
	if !create && (change.OrganizationID != nil || change.APIKey != nil) {
		if _, err = h.kiloStateSource(); err != nil {
			kiloAccountError(c, err)
			return
		}
	}
	account, err := h.authManager.ChangeKiloAccount(c.Request.Context(), c.Param("id"), change, create, false)
	if err != nil {
		kiloAccountError(c, err)
		return
	}
	if h.postAuthPersistHook != nil {
		if err = h.postAuthPersistHook(c.Request.Context(), account); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kilo account saved; runtime registration is pending"})
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
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	c.JSON(status, h.kiloAccountView(account, entry))
}
func (h *Handler) DeleteKiloAccount(c *gin.Context) {
	if h.authManager == nil {
		kiloAccountError(c, coreauth.ErrKiloAccountStorage)
		return
	}
	if _, err := h.authManager.ChangeKiloAccount(c.Request.Context(), c.Param("id"), coreauth.KiloAccountChanges{}, false, true); err != nil {
		kiloAccountError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
