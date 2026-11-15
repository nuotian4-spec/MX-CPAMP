package kilo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

func (h *Handler) probesRequest(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/kilo/accounts/") || !strings.HasSuffix(r.URL.Path, "/probes") {
		return false
	}
	if r.Method != "GET" && r.Method != "PUT" {
		response.MethodNotAllowed(w)
		return true
	}
	if r.URL.RawQuery != "" {
		response.JSON(w, 400, map[string]string{"error": "探测配置不接受查询参数"})
		return true
	}
	fields := map[string]json.RawMessage{}
	ref := ""
	if r.Method == "GET" {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(raw) > 0 {
			response.JSON(w, 400, map[string]string{"error": "探测状态读取不接受正文"})
			return true
		}
	} else {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		var trailing any
		if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&trailing) != io.EOF || json.Unmarshal(fields["connection_ref"], &ref) != nil {
			response.JSON(w, 400, map[string]string{"error": "探测保存需要单个JSON对象和连接引用"})
			return true
		}
		delete(fields, "connection_ref")
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/kilo/accounts/"), "/probes")
	result, err := h.Service.Probes(r.Context(), r.Method, id, ref, fields)
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	response.JSON(w, 200, result)
	return true
}
