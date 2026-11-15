package kilo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

type Handler struct {
	Service *svc.Service
	Auth    middleware.AdminVerifier
}

// Handle处理Kilo管理接口，所有操作沿用管理员鉴权。
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !middleware.AuthorizeAdmin(w, r, h.Auth) {
		return
	}
	if h.probesRequest(w, r) {
		return
	}
	if h.manualRequest(w, r) {
		return
	}
	if h.quotaRequest(w, r) {
		return
	}
	if h.deviceRequest(w, r) {
		return
	}
	if h.organizationsRequest(w, r) {
		return
	}
	if h.proxyRequest(w, r) {
		return
	}
	if r.URL.Path == "/api/kilo/accounts" && r.Method == http.MethodPost {
		h.writeAccount(w, r, "")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/kilo/accounts/") {
		id := strings.TrimPrefix(r.URL.Path, "/api/kilo/accounts/")
		if r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			response.MethodNotAllowed(w)
			return
		}
		h.writeAccount(w, r, id)
		return
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	var result any
	var err error
	switch r.URL.Path {
	case "/api/kilo/accounts":
		result, err = h.Service.Accounts(r.Context())
	case "/api/kilo/status":
		result, err = h.Service.Status(r.Context())
	case "/api/kilo/capabilities":
		result, err = h.Service.Capabilities(r.Context())
	default:
		response.JSON(w, 404, map[string]string{"error": "接口不存在"})
		return
	}
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) writeAccount(w http.ResponseWriter, r *http.Request, id string) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	if decoder.Decode(&fields) != nil || fields == nil {
		response.JSON(w, 400, map[string]string{"code": "invalid_account_input", "error": "账号请求应为JSON对象"})
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		response.JSON(w, 400, map[string]string{"code": "invalid_account_input", "error": "账号请求格式无效"})
		return
	}
	var reference string
	if json.Unmarshal(fields["connection_ref"], &reference) != nil {
		response.JSON(w, 400, map[string]string{"code": "connection_reference_required", "error": "请先刷新账号列表取得连接引用"})
		return
	}
	delete(fields, "connection_ref")
	result, err := h.Service.WriteAccount(r.Context(), r.Method, id, reference, fields)
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	response.JSON(w, status, result)
}
