package siwc

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/siwc"
)

type Handler struct {
	Service *svc.Service
	Auth    middleware.AdminVerifier
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !middleware.AuthorizeAdmin(w, r, h.Auth) {
		return
	}
	if h.Service == nil {
		response.JSON(w, 503, map[string]string{"code": "cpa_unavailable", "error": "CPA连接暂不可用"})
		return
	}
	query := r.URL.Query()
	identity := model.SIWCIdentity{ConnectionRef: query.Get("connection_ref"), AuthID: query.Get("auth_id"), AuthIndex: query.Get("auth_index")}
	input := model.SIWCSessionInput{SIWCIdentity: identity, State: query.Get("state")}
	var result any
	var err error
	switch r.URL.Path {
	case "/api/siwc/credential":
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		result, err = h.Service.Credential(r.Context(), identity)
	case "/api/siwc/bind":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		var body model.SIWCIdentity
		if !decode(w, r, &body) {
			return
		}
		result, err = h.Service.Bind(r.Context(), body)
	case "/api/siwc/callback":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		var body model.SIWCCallbackInput
		if !decode(w, r, &body) {
			return
		}
		err = h.Service.Callback(r.Context(), body)
		result = map[string]any{"status": "wait", "submitted": true}
	case "/api/siwc/status":
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		result, err = h.Service.Status(r.Context(), input)
	case "/api/siwc/session":
		if r.Method != http.MethodDelete {
			response.MethodNotAllowed(w)
			return
		}
		result, err = h.Service.Cancel(r.Context(), input)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		response.JSON(w, 400, map[string]string{"code": "siwc_input_invalid", "error": "SIWC输入格式无效"})
		return false
	}
	return true
}
