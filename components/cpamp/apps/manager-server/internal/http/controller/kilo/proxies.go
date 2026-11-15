package kilo

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

func (h *Handler) proxyRequest(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	kind, id := "", ""
	switch {
	case path == "/api/kilo/proxies/preview":
		kind = "proxy-preview"
	case path == "/api/kilo/proxies":
		kind = "proxies"
	case strings.HasPrefix(path, "/api/kilo/proxies/") && strings.HasSuffix(path, "/recheck"):
		kind = "proxy-recheck"
		id = strings.TrimSuffix(strings.TrimPrefix(path, "/api/kilo/proxies/"), "/recheck")
	case path == "/api/kilo/proxy-recycle":
		kind = "proxy-recycle"
	case strings.HasPrefix(path, "/api/kilo/proxy-recycle/") && strings.HasSuffix(path, "/restore"):
		kind = "proxy-restore"
		id = strings.TrimSuffix(strings.TrimPrefix(path, "/api/kilo/proxy-recycle/"), "/restore")
	case strings.HasPrefix(path, "/api/kilo/proxies/"):
		kind = "proxies"
		id = strings.TrimPrefix(path, "/api/kilo/proxies/")
	case path == "/api/kilo/proxy-pool":
		kind = "proxy-pool"
	case strings.HasPrefix(path, "/api/kilo/accounts/") && strings.HasSuffix(path, "/egress"):
		kind = "egress"
		id = strings.TrimSuffix(strings.TrimPrefix(path, "/api/kilo/accounts/"), "/egress")
	default:
		return false
	}
	if (kind == "proxies" || kind == "proxy-recycle") && id == "" && r.Method == http.MethodGet {
		read := h.Service.Proxies
		if kind == "proxy-recycle" {
			read = h.Service.ProxyRecycle
		}
		result, err := read(r.Context())
		if err != nil {
			status, code, message := svc.HTTPError(err)
			response.JSON(w, status, map[string]string{"code": code, "error": message})
		} else {
			response.JSON(w, 200, result)
		}
		return true
	}
	allowed := kind == "proxy-preview" && r.Method == http.MethodPost || (kind == "proxy-restore" || kind == "proxy-recheck") && r.Method == http.MethodPost || kind == "proxy-recycle" && r.Method == http.MethodDelete || kind == "proxies" && ((id == "" && r.Method == http.MethodPost) || (id != "" && (r.Method == http.MethodPatch || r.Method == http.MethodDelete))) || kind == "proxy-pool" && r.Method == http.MethodPatch || kind == "egress" && r.Method == http.MethodPut
	if !allowed {
		response.MethodNotAllowed(w)
		return true
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	var trailing any
	if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&trailing) != io.EOF {
		response.JSON(w, 400, map[string]string{"error": "代理请求应为单个JSON对象"})
		return true
	}
	var ref string
	if json.Unmarshal(fields["connection_ref"], &ref) != nil {
		response.JSON(w, 400, map[string]string{"error": "请先刷新代理列表取得连接引用"})
		return true
	}
	delete(fields, "connection_ref")
	if kind == "proxy-preview" {
		result, err := h.Service.PreviewProxy(r.Context(), ref, fields)
		if err != nil {
			status, code, message := svc.HTTPError(err)
			response.JSON(w, status, map[string]string{"code": code, "error": message})
		} else {
			response.JSON(w, http.StatusOK, result)
		}
		return true
	}
	result, err := h.Service.WriteProxies(r.Context(), r.Method, kind, id, ref, fields)
	if err != nil {
		var duplicate *svc.ProxyIPConflict
		if errors.As(err, &duplicate) {
			body := map[string]string{"code": "proxy_ip_already_registered", "error": "proxy_ip_already_registered"}
			if duplicate.IP != "" {
				body["ip"] = duplicate.IP
			}
			if duplicate.ProxyID != "" {
				body["proxy_id"] = duplicate.ProxyID
			}
			response.JSON(w, http.StatusConflict, body)
			return true
		}
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	status := http.StatusOK
	if r.Method == http.MethodPost && kind == "proxies" && id == "" {
		status = http.StatusCreated
	}
	response.JSON(w, status, result)
	return true
}
