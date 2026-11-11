package bpssettings

import (
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"net/http"
)

func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !middleware.AuthorizeAdmin(w, r, h.Auth) {
		return
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	q := r.URL.Query()
	if q.Get("auth_index") == "" || q.Get("connection_ref") == "" {
		response.JSON(w, 400, map[string]string{"error": "请选择凭证并刷新连接"})
		return
	}
	result, err := h.Service.GetBPSUsage(r.Context(), q.Get("auth_index"), q.Get("auth_id"), q.Get("connection_ref"))
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return
	}
	response.JSON(w, 200, result)
}
