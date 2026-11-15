package kilo

import (
	"encoding/json"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
	"io"
	"net/http"
	"strings"
)

func (h *Handler) quotaRequest(w http.ResponseWriter, r *http.Request) bool {
	action, id := "", ""
	path := r.URL.Path
	if path == "/api/kilo/quota-sources" {
		action = "sources"
	} else if strings.HasPrefix(path, "/api/kilo/accounts/") {
		for suffix, candidate := range map[string]string{"/quota": "get", "/quota-source": "source", "/quota/query": "query", "/source-policy": "policy"} {
			if strings.HasSuffix(path, suffix) {
				id = strings.TrimSuffix(strings.TrimPrefix(path, "/api/kilo/accounts/"), suffix)
				action = candidate
				break
			}
		}
	}
	if action == "" {
		return false
	}
	if action == "policy" {
		switch r.Method {
		case http.MethodGet:
			action = "policy_get"
		case http.MethodPut:
			action = "policy_put"
		default:
			response.MethodNotAllowed(w)
			return true
		}
	} else if action == "source" {
		switch r.Method {
		case http.MethodPut:
			action = "put"
		case http.MethodDelete:
			action = "delete"
		default:
			response.MethodNotAllowed(w)
			return true
		}
	} else if (action == "query" && r.Method != http.MethodPost) || (action != "query" && r.Method != http.MethodGet) {
		response.MethodNotAllowed(w)
		return true
	}
	fields := map[string]json.RawMessage{}
	reference := ""
	if r.URL.RawQuery != "" {
		response.JSON(w, 400, map[string]string{"error": "额度接口不接受查询参数"})
		return true
	}
	if action == "sources" || action == "get" || action == "policy_get" {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(raw) != 0 {
			response.JSON(w, 400, map[string]string{"error": "额度读取不接受正文"})
			return true
		}
	} else {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
		var trailing any
		if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&trailing) != io.EOF || json.Unmarshal(fields["connection_ref"], &reference) != nil {
			response.JSON(w, 400, map[string]string{"error": "额度请求需要单个JSON对象和连接引用"})
			return true
		}
		delete(fields, "connection_ref")
	}
	result, err := h.Service.Quota(r.Context(), action, id, reference, fields)
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	if result.Policy != nil {
		response.JSON(w, result.Status, result.Policy)
	} else if result.Sources != nil {
		response.JSON(w, result.Status, result.Sources)
	} else {
		response.JSON(w, result.Status, result.View)
	}
	return true
}
