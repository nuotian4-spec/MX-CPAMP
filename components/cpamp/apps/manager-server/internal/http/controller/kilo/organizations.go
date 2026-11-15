package kilo

import (
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

func (h *Handler) organizationsRequest(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/kilo/accounts/"
	const suffix = "/organizations"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
		return false
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(raw) != 0 || r.URL.RawQuery != "" {
		response.JSON(w, 400, map[string]string{"error": "组织查询不接受正文或查询参数"})
		return true
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
	result, err := h.Service.Organizations(r.Context(), id)
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	response.JSON(w, 200, result)
	return true
}
