package proxy

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	concurrency "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/credentialconcurrency"
)

const (
	cpaV8Imports    = "/v8/management/plugins/imports"
	cpaV8CoreUpdate = "/v8/management/updates/core"
	cpaV8Operations = "/v8/management/updates/operations"
)

// IsCPAV8UpdatePath matches only the update center's explicit endpoint shapes.
func IsCPAV8UpdatePath(path string) bool { return len(v8UpdateMethods(path)) != 0 }

func v8UpdateMethods(path string) []string {
	switch path {
	case cpaV8Imports, cpaV8CoreUpdate + "/previews":
		return []string{http.MethodPost}
	case cpaV8CoreUpdate:
		return []string{http.MethodGet}
	}
	for _, prefix := range []string{cpaV8Imports, cpaV8CoreUpdate + "/previews", cpaV8Operations} {
		if !strings.HasPrefix(path, prefix+"/") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(path, prefix+"/"), "/")
		if parts[0] == "" || parts[0] == "." || parts[0] == ".." {
			return nil
		}
		if len(parts) == 1 {
			switch prefix {
			case cpaV8Imports:
				return []string{http.MethodGet, http.MethodDelete}
			case cpaV8Operations:
				return []string{http.MethodGet}
			}
		}
		if len(parts) == 2 && parts[1] == "install" && prefix != cpaV8Operations {
			return []string{http.MethodPost}
		}
		return nil
	}
	return nil
}

// ProxyV8Updates preserves the v8 path and body while using the saved CPA key.
func (s *Service) ProxyV8Updates(w http.ResponseWriter, r *http.Request, connections *concurrency.Service, writeError func(http.ResponseWriter, int, error), methodNotAllowed func(http.ResponseWriter)) {
	methods := v8UpdateMethods(r.URL.Path)
	if len(methods) == 0 || !isCanonicalRequestPath(r.URL) {
		writeError(w, http.StatusNotFound, errors.New("proxy path must be an update-center endpoint"))
		return
	}
	if !slices.Contains(methods, r.Method) {
		methodNotAllowed(w)
		return
	}
	if connections == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("update connection reference is unavailable"))
		return
	}
	setup, ok, err := s.resolveSetup(r.Context())
	if err != nil || !ok {
		writeError(w, http.StatusServiceUnavailable, errors.New("CPA connection is not configured"))
		return
	}
	reference := r.Header.Get(ConfigConnectionHeader)
	if reference == "required" && r.Method == http.MethodGet {
		reference, err = connections.BindConnection(r.Context(), setup)
		if err != nil {
			writeError(w, http.StatusConflict, errors.New("CPA connection changed; reopen the update page"))
			return
		}
	}
	if len(reference) != 32 {
		writeError(w, http.StatusBadRequest, errors.New("update request requires a connection reference"))
		return
	}
	if err := connections.CheckConnection(r.Context(), reference, setup); err != nil {
		writeError(w, http.StatusConflict, errors.New("CPA connection changed; reopen the update page"))
		return
	}
	guard := &configYAMLGuard{connections: connections, reference: reference, setup: setup}
	s.proxyToSavedSetup(w, r, writeError, true, false, guard)
}
