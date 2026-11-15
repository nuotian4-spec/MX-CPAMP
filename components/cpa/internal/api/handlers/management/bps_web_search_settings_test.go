package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

func TestBPSWebSearchSettingsPersistence(t *testing.T) {
	h, path, _ := newIssue6631Handler(t, "auth-dir: auths\n")
	router := gin.New()
	router.GET("/settings", h.GetBPSSettings)
	router.PUT("/settings", h.PutBPSSettings)
	get := func() bool {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings", nil))
		return ojson.GetBytes(w.Body.Bytes(), "web_search").Bool()
	}
	if !get() {
		t.Fatal("default search disabled")
	}
	base := `{"enabled":true,"images_enabled":true,"image_transport":"native","image_relay_base_url":""`
	putIssue6631Value(t, router, "/settings", base+`,"web_search":false}`)
	if get() || readIssue6631Disk(t, path).BPS.SearchEnabled() {
		t.Fatal("false did not persist")
	}
	putIssue6631Value(t, router, "/settings", base+`}`)
	if get() || readIssue6631Disk(t, path).BPS.SearchEnabled() {
		t.Fatal("legacy PUT reset false")
	}
	putIssue6631Value(t, router, "/settings", base+`,"web_search":true}`)
	if !get() || !readIssue6631Disk(t, path).BPS.SearchEnabled() {
		t.Fatal("true did not persist")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(base+`,"web_search":"false"}`)))
	if w.Code != 400 {
		t.Fatal("invalid bool accepted")
	}
}
