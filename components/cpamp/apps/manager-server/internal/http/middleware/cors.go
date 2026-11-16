package middleware

import (
	"net/http"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/config"
)

func WithCORS(cfg config.Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteCORS(cfg, w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func WriteCORS(cfg config.Config, w http.ResponseWriter, r *http.Request) {
	if len(cfg.CORSOrigins) == 0 {
		return
	}
	origin := r.Header.Get("Origin")
	allowed := cfg.CORSOrigins[0]
	for _, candidate := range cfg.CORSOrigins {
		if candidate == "*" || candidate == origin {
			allowed = candidate
			break
		}
	}
	if allowed == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if origin != "" && allowed == origin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Usage-Import-Prefix-SHA256")
	if strings.HasPrefix(r.URL.Path, "/v8/management/updates/") || strings.HasPrefix(r.URL.Path, "/v8/management/plugins/imports") {
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CPA-Connection-Ref")
		w.Header().Set("Access-Control-Expose-Headers", "X-CPA-Connection-Ref")
	}
	if r.URL.Path == "/v0/management/config.yaml" {
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match, X-CPA-Connection-Ref")
		w.Header().Set("Access-Control-Expose-Headers", "ETag, X-CPA-Connection-Ref")
	}
}
