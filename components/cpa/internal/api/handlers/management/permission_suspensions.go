package management

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) addPermissionSnapshot(entry gin.H, auth *coreauth.Auth) {
	entry["permission_suspensions"] = nil
	entry["permission_suspensions_persistence"] = "unknown"
	if auth == nil {
		return
	}
	entry["auth_id"] = auth.ID
	entry["provider"] = auth.Provider
	entry["registration_epoch"] = strconv.FormatUint(auth.RegistrationEpoch, 10)
	entry["credential_version"] = strconv.FormatUint(auth.CredentialVersion, 10)
	entry["generation"] = strconv.FormatUint(auth.Generation, 10)
	if h.authManager == nil || h.authManager.HomeEnabled() {
		return
	}
	status := h.authManager.PermissionPersistenceStatus()
	entry["permission_suspensions_persistence"] = status
	if status != "unknown" {
		entry["permission_suspensions"] = coreauth.PermissionSuspensionSnapshot(auth)
	}
}

func (h *Handler) ReleasePermissionSuspensions(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(503, gin.H{"error": "permission state is unavailable"})
		return
	}
	var request struct {
		AuthID     string                     `json:"auth_id"`
		Provider   string                     `json:"provider"`
		Epoch      string                     `json:"expected_registration_epoch"`
		Version    string                     `json:"expected_credential_version"`
		Generation string                     `json:"expected_generation"`
		Scopes     []coreauth.PermissionScope `json:"scopes"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"error": "invalid permission release"})
		return
	}
	epoch, errEpoch := strconv.ParseUint(request.Epoch, 10, 64)
	version, errVersion := strconv.ParseUint(request.Version, 10, 64)
	generation, errGeneration := strconv.ParseUint(request.Generation, 10, 64)
	if errEpoch != nil || errVersion != nil || errGeneration != nil {
		c.JSON(400, gin.H{"error": "invalid permission snapshot version"})
		return
	}
	auth, err := h.authManager.ReleasePermissionSuspensions(c.Request.Context(), request.AuthID, request.Provider, epoch, version, generation, request.Scopes)
	if err != nil {
		status := http.StatusInternalServerError
		var coded interface{ StatusCode() int }
		if errors.As(err, &coded) {
			status = coded.StatusCode()
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	entry := gin.H{"auth_id": auth.ID, "provider": auth.Provider}
	h.addPermissionSnapshot(entry, auth)
	c.JSON(200, entry)
}
