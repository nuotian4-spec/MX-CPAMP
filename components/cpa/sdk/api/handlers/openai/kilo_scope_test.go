package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
)

func TestKiloScopeRejectsWebsocketBeforeUpgrade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	c.Request = req.WithContext(channelscope.With(req.Context(), "kilo"))
	(&OpenAIResponsesAPIHandler{}).ResponsesWebsocket(c)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ojson.Get(rec.Body.String(), "error.code").String() != "unsupported_transport" || strings.Contains(strings.ToLower(rec.Body.String()), "kilo") {
		t.Fatalf("uncontrolled upgrade rejection: %s", rec.Body.String())
	}
	if rec.Header().Get("Upgrade") != "" {
		t.Fatal("rejected request upgraded")
	}
}
