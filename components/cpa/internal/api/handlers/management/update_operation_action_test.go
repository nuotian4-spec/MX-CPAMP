package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStoreInstallEarlyFailureKeepsAction(t *testing.T) {
	for _, route := range []string{"/v8/management/plugins/store/:id/install", "/v0/management/plugin-store/:id/install"} {
		t.Run(route, func(t *testing.T) {
			h := &Handler{}
			registerTestUpdateSite(t, h)
			router := gin.New()
			router.POST(route, func(c *gin.Context) {
				mutation, ok := h.ensureUpdateMutation(c)
				if !ok {
					return
				}
				c.AbortWithStatus(http.StatusBadRequest)
				h.finishUpdateRequest(c, mutation)
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, strings.ReplaceAll(route, ":id", "cpa-codex-candy-eval"), nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: %d", w.Code)
			}
			op, err := h.updateStateStore.Get(w.Header().Get("X-Update-Operation-ID"))
			if err != nil || op.Action != "install" || op.PluginID != "cpa-codex-candy-eval" || op.Status != "failed" {
				t.Fatalf("operation: %+v, error: %v", op, err)
			}
		})
	}
}
