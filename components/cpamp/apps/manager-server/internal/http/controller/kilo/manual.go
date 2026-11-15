package kilo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

func (h *Handler) manualRequest(w http.ResponseWriter, r *http.Request) bool {
	models := r.URL.Path == "/api/kilo/test-models"
	if !models && !(strings.HasPrefix(r.URL.Path, "/api/kilo/accounts/") && strings.HasSuffix(r.URL.Path, "/test")) {
		return false
	}
	if (models && r.Method != "GET") || (!models && r.Method != "POST") {
		response.MethodNotAllowed(w)
		return true
	}
	if r.URL.RawQuery != "" {
		response.JSON(w, 400, map[string]string{"error": "手动测试不接受查询参数"})
		return true
	}
	var result any
	var err error
	if models {
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, 1))
		if readErr != nil || len(raw) > 0 {
			response.JSON(w, 400, map[string]string{"error": "目录读取不接受正文"})
			return true
		}
		result, err = h.Service.TestModels(r.Context())
	} else {
		var fields map[string]json.RawMessage
		var trailing any
		var ref string
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
		if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&trailing) != io.EOF || json.Unmarshal(fields["connection_ref"], &ref) != nil {
			response.JSON(w, 400, map[string]string{"error": "手动测试需要单个JSON对象和连接引用"})
			return true
		}
		delete(fields, "connection_ref")
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/kilo/accounts/"), "/test")
		result, err = h.Service.ManualTest(r.Context(), id, ref, fields)
	}
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	response.JSON(w, 200, result)
	return true
}
