package oauthlogin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	login "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/oauthlogin"
)

type Handler struct {
	Service *login.Service
	Auth    middleware.AdminVerifier
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !middleware.AuthorizeAdmin(w, r, h.Auth) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/oauth-login/")
	var result any
	var err error
	switch {
	case path == "batches" && r.Method == http.MethodPost:
		var input login.BatchInput
		if !decodeLimit(w, r, &input, 1024*1024) {
			return
		}
		result, err = h.Service.CreateBatch(input)
	case path == "batches/active" && r.Method == http.MethodGet:
		result, err = h.Service.ActiveBatch()
	case strings.HasPrefix(path, "batches/") && !strings.Contains(strings.TrimPrefix(path, "batches/"), "/"):
		id := strings.TrimPrefix(path, "batches/")
		if r.Method == http.MethodGet {
			result, err = h.Service.GetBatch(id)
		} else if r.Method == http.MethodDelete {
			result, err = h.Service.CancelBatch(id)
		} else {
			response.MethodNotAllowed(w)
			return
		}
	case path == "tasks" && r.Method == http.MethodPost:
		var input login.Input
		if !decode(w, r, &input) {
			return
		}
		result, err = h.Service.Create(input)
	case strings.HasPrefix(path, "tasks/") && !strings.Contains(strings.TrimPrefix(path, "tasks/"), "/"):
		id := strings.TrimPrefix(path, "tasks/")
		if r.Method == http.MethodGet {
			result, err = h.Service.Get(id)
		} else if r.Method == http.MethodDelete {
			result, err = h.Service.Cancel(id)
		} else {
			response.MethodNotAllowed(w)
			return
		}
	case strings.HasPrefix(path, "oauth/"):
		action := strings.TrimPrefix(path, "oauth/")
		input := map[string]string{"state": r.URL.Query().Get("state"), "provider": r.URL.Query().Get("provider")}
		if r.Method == http.MethodPost {
			if !decode(w, r, &input) {
				return
			}
		}
		result, err = h.Service.OAuth(r.Context(), r.Method, action, input)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, login.ErrInput):
			status = http.StatusBadRequest
		case errors.Is(err, login.ErrBusy), errors.Is(err, login.ErrImporting):
			status = http.StatusConflict
		case errors.Is(err, login.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, login.ErrUnavailable):
			status = http.StatusServiceUnavailable
		}
		response.JSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, result)
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeLimit(w, r, target, 32*1024)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "登录输入无效"})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "登录输入无效"})
		return false
	}
	return true
}
