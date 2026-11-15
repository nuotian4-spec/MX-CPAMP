package cliproxy

import (
	"context"
	"fmt"
	"github.com/MoeForge/moego/ojson"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each fixture IP is an explicit synthetic identity observed through real CONNECT/TLS.
func i1NativeIPs(t *testing.T, f *kiloNativeFixture, n int, upstream func(int, http.ResponseWriter, *http.Request)) []string {
	t.Helper()
	old := http.DefaultTransport
	transport := old.(*http.Transport).Clone()
	allowed := map[string]bool{}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowed[addr] {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		return old.(*http.Transport).DialContext(ctx, network, addr)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = old; transport.CloseIdleConnections() })
	state := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	revision := ojson.GetBytes(state, "revision").Uint()
	state = m2Request(t, f, "PATCH", kiloProxiesURL+"/local-proxy", fmt.Sprintf(`{"expected_revision":%d,"enabled":false}`, revision), 200)
	ids := []string{}
	for i := 0; i < n; i++ {
		idx := i
		ip := fmt.Sprintf("203.0.113.%d", i+1)
		target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "api.kilo.ai" && r.URL.Path == "/api/profile" && f.profileHandler != nil {
				f.profileHandler(w, r)
				return
			}
			if r.Host == "api.kilo.ai" && f.deviceHandler != nil {
				f.deviceHandler(w, r)
				return
			}
			if r.Host == "api.ipify.org" {
				_, _ = io.WriteString(w, ip)
				return
			}
			if r.Host != "ai-gateway.kilo.ai" || r.Method != "POST" || (r.URL.Path != "/api/v1/responses" && !(f.allowAutoFree && r.URL.Path == "/api/v1/chat/completions")) {
				t.Errorf("unexpected route %s %s%s", r.Method, r.Host, r.URL.Path)
				http.Error(w, "route", 500)
				return
			}
			if r.Header.Get("Authorization") != "Bearer synthetic-kilo-token" {
				t.Error("credential not injected")
			}
			upstream(idx, w, r)
		}))
		t.Cleanup(target.Close)
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "CONNECT" || (r.Host != "api.ipify.org:443" && r.Host != "ai-gateway.kilo.ai:443" && r.Host != "api.kilo.ai:443") {
				http.Error(w, "target", 400)
				return
			}
			remote, err := net.Dial("tcp", target.Listener.Addr().String())
			if err != nil {
				http.Error(w, "local target", 502)
				return
			}
			client, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				remote.Close()
				return
			}
			_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = rw.Flush()
			go func() { _, _ = io.Copy(remote, rw); _ = remote.Close(); _ = client.Close() }()
			_, _ = io.Copy(client, remote)
			_ = remote.Close()
			_ = client.Close()
		}))
		t.Cleanup(proxy.Close)
		allowed[proxy.Listener.Addr().String()] = true
		revision = ojson.GetBytes(state, "revision").Uint()
		state = m2Request(t, f, "POST", kiloProxiesURL, fmt.Sprintf(`{"expected_revision":%d,"url":%q,"name":%q}`, revision, proxy.URL, ip), 201)
		entries := ojson.GetBytes(state, "proxies").Array()
		id := entries[len(entries)-1].Get("id").String()
		ids = append(ids, id)
		revision = ojson.GetBytes(state, "revision").Uint()
		state = m2Request(t, f, "POST", kiloProxiesURL+"/"+id+"/recheck", fmt.Sprintf(`{"expected_revision":%d}`, revision), 200)
		revision = ojson.GetBytes(state, "revision").Uint()
		state = m2Request(t, f, "PATCH", kiloProxiesURL+"/"+id, fmt.Sprintf(`{"expected_revision":%d,"fixed_exit_ip":%q}`, revision, ip), 200)
	}
	revision = ojson.GetBytes(state, "revision").Uint()
	m2Request(t, f, "PATCH", "/v8/management/kilo/proxy-pool", fmt.Sprintf(`{"expected_revision":%d,"auto_assignment_enabled":true,"max_unique_accounts":10}`, revision), 200)
	return ids
}
