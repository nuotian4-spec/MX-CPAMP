package proxy

import (
	"errors"
	"net/http"

	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const ConfigConnectionHeader = "X-CPA-Connection-Ref"

type configYAMLGuard struct {
	connections *concurrency.Service
	reference   string
	setup       store.Setup
	changed     bool
}

// ProxyConfigYAML复用已有连接引用，不替换旧无引用调用的兼容语义。
func (s *Service) ProxyConfigYAML(w http.ResponseWriter, r *http.Request, connections *concurrency.Service, writeError func(http.ResponseWriter, int, error)) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, errors.New("配置事务仅接受GET和PUT"))
		return
	}
	if r.Method == http.MethodPut && r.Header.Get("If-Match") == "" {
		writeError(w, http.StatusBadRequest, errors.New("配置事务缺少版本条件"))
		return
	}
	if connections == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("连接引用暂不可用"))
		return
	}
	setup, ok, err := s.resolveSetup(r.Context())
	if err != nil || !ok {
		writeError(w, http.StatusServiceUnavailable, errors.New("管理连接未配置"))
		return
	}
	reference := r.Header.Get(ConfigConnectionHeader)
	if reference == "required" && r.Method == http.MethodGet {
		snapshot, err := connections.List(r.Context())
		if err != nil || snapshot.Mode != "local" || snapshot.ConnectionRef == "" {
			writeError(w, http.StatusServiceUnavailable, errors.New("连接引用暂不可用"))
			return
		}
		reference = snapshot.ConnectionRef
	}
	if len(reference) != 32 {
		writeError(w, http.StatusBadRequest, errors.New("配置事务缺少有效连接引用"))
		return
	}
	if err := connections.CheckConnection(r.Context(), reference, setup); err != nil {
		writeError(w, http.StatusConflict, errors.New("配置管理连接已变化，未发送保存"))
		return
	}
	guard := &configYAMLGuard{connections: connections, reference: reference, setup: setup}
	s.proxyToSavedSetup(w, r, writeError, true, false, guard)
}
