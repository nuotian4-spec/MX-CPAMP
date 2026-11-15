package helps

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestKiloProfileExpiredHealthReportsPending(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer proxy.Close()
	r, err := NewKiloRuntime(config.KiloConfig{ProxyLimit: 1, Proxies: []config.KiloProxy{{ID: "p", URL: proxy.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	r.health["p"] = kiloProxyHealth{checked: true, healthy: true, checkedAt: time.Now().Add(-2 * time.Minute)}
	_ = r.pool.SetHealthy("p", true)
	client, release, err := r.AcquireProfile("account")
	if release != nil {
		release()
	}
	if client != nil || !errors.Is(err, ErrKiloProfileHealthPending) {
		t.Fatalf("expired health must remain pending, not a terminal profile failure: client=%v error=%v", client, err)
	}
}
