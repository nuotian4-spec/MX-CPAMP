package kilo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	svc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/kilo"
)

func (h *Handler) deviceRequest(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/kilo/device-auth"
	if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
		return false
	}
	action, id := "", ""
	if r.URL.Path == prefix && r.Method == http.MethodPost {
		action = "start"
	} else if strings.HasPrefix(r.URL.Path, prefix+"/") {
		id = strings.TrimPrefix(r.URL.Path, prefix+"/")
		if r.Method == http.MethodDelete {
			action = "cancel"
		} else if r.Method == http.MethodPost {
			for _, suffix := range []string{"poll", "save"} {
				if strings.HasSuffix(id, "/"+suffix) {
					action = suffix
					id = strings.TrimSuffix(id, "/"+suffix)
					break
				}
			}
		}
	}
	if action == "" {
		response.MethodNotAllowed(w)
		return true
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
	var trailing any
	if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&trailing) != io.EOF {
		response.JSON(w, 400, map[string]string{"error": "授权请求应为单个JSON对象"})
		return true
	}
	var ref string
	if json.Unmarshal(fields["connection_ref"], &ref) != nil {
		response.JSON(w, 400, map[string]string{"error": "授权请求需要当前连接引用"})
		return true
	}
	delete(fields, "connection_ref")
	result, err := h.Service.Device(r.Context(), action, id, ref, fields)
	if err != nil {
		status, code, message := svc.HTTPError(err)
		response.JSON(w, status, map[string]string{"code": code, "error": message})
		return true
	}
	status := 200
	if action == "save" {
		status = 201
	}
	response.JSON(w, status, result)
	return true
}
