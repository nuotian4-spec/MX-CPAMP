package bpssettings

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
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
	var result any
	var err error
	switch r.Method {
	case http.MethodGet:
		result, err = h.Service.GetBPSSettings(r.Context())
	case http.MethodPut:
		var input svc.BPSSettingsInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			response.JSON(w, 400, map[string]string{"error": "BPS设置输入无效"})
			return
		}
		err = h.Service.PutBPSSettings(r.Context(), input)
		result = map[string]string{"status": "saved"}
	default:
		response.MethodNotAllowed(w)
		return
	}
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return
	}
	response.JSON(w, 200, result)
}
