package cliproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type kiloNativeHook struct {
	coreauth.NoopHook
	mu      sync.Mutex
	results []coreauth.Result
}

func (h *kiloNativeHook) OnResult(_ context.Context, result coreauth.Result) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, result)
}
func (h *kiloNativeHook) snapshot() []coreauth.Result {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]coreauth.Result(nil), h.results...)
}

type kiloNativeFixture struct {
	unexpectedProxy      atomic.Int32
	cfg                  *config.Config
	probeHandler         http.HandlerFunc
	deviceHandler        http.HandlerFunc
	deviceProxyAuth      func(string)
	profileHandler       http.HandlerFunc
	usageHandler         http.HandlerFunc
	oauthHandler         http.HandlerFunc
	usageDirect          atomic.Int32
	usageConnects        atomic.Int32
	oauthConnects        atomic.Int32
	deviceConnects       atomic.Int32
	deviceDirect         atomic.Int32
	postPersistGate      func(context.Context, *coreauth.Auth)
	server               *httptest.Server
	manager              *coreauth.Manager
	executor             *native.KiloExecutor
	hook                 *kiloNativeHook
	expectedToken        atomic.Value
	expectedOrganization atomic.Value
	posts                atomic.Int32
	connects             atomic.Int32
	probes               atomic.Int32
	requests             chan []byte
	allowDirect          bool
	allowAutoFree        bool
	configPath           string
}

func kiloAccountForMachine(t *testing.T, f *kiloNativeFixture, machine string) string {
	t.Helper()
	for _, a := range f.manager.List() {
		if coreauth.KiloMachineID(a) == machine {
			return a.ID
		}
	}
	t.Fatalf("observed machine has no persisted account: %s", machine)
	return ""
}

// This fixture keeps the production URL, executor and transport. Only the
// CONNECT proxy's destination and the test trust root point at loopback servers.
func newKiloNativeFixture(t *testing.T, upstreamHandler http.HandlerFunc, empty ...bool) *kiloNativeFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("MANAGEMENT_PASSWORD", "synthetic-management-key")
	f := &kiloNativeFixture{hook: &kiloNativeHook{}, requests: make(chan []byte, 10)}
	f.expectedToken.Store("synthetic-kilo-token")
	f.expectedOrganization.Store("")
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "chatgpt.com" && f.usageHandler != nil {
			f.usageHandler(w, r)
			return
		}
		if r.Host == "auth.openai.com" && f.oauthHandler != nil {
			f.oauthHandler(w, r)
			return
		}
		if r.Host == "api.kilo.ai" {
			if r.URL.Path == "/api/profile" && f.profileHandler != nil {
				f.profileHandler(w, r)
				return
			}
			if f.deviceHandler == nil {
				t.Error("unexpected device request")
				http.Error(w, "unexpected", 500)
				return
			}
			f.deviceHandler(w, r)
			return
		}
		if r.Method == http.MethodGet && r.Host == "api.ipify.org" {
			f.probes.Add(1)
			if f.probeHandler != nil {
				f.probeHandler(w, r)
				return
			}
			_, _ = io.WriteString(w, "127.0.0.2")
			return
		}
		f.posts.Add(1)
		if r.Method != "POST" || (r.URL.Path != "/api/v1/responses" && !(f.allowAutoFree && r.URL.Path == "/api/v1/chat/completions")) {
			t.Errorf("upstream route=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+f.expectedToken.Load().(string) {
			t.Error("synthetic credential not injected")
		}
		if r.Header.Get("X-KiloCode-OrganizationId") != f.expectedOrganization.Load().(string) {
			t.Error("organization header does not match selected space")
		}
		body, _ := io.ReadAll(r.Body)
		f.requests <- body
		r.Body = io.NopCloser(bytes.NewReader(body))
		upstreamHandler(w, r)
	}))
	t.Cleanup(upstream.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || (r.Host != "ai-gateway.kilo.ai:443" && r.Host != "api.ipify.org:443" && r.Host != "api.kilo.ai:443" && r.Host != "chatgpt.com:443" && r.Host != "auth.openai.com:443") {
			f.unexpectedProxy.Add(1)
			t.Errorf("unexpected proxy request %s %s", r.Method, r.Host)
			http.Error(w, "bad proxy target", 400)
			return
		}
		if r.Host == "ai-gateway.kilo.ai:443" {
			f.connects.Add(1)
		}
		if r.Host == "api.kilo.ai:443" {
			f.deviceConnects.Add(1)
			if f.deviceProxyAuth != nil {
				f.deviceProxyAuth(r.Header.Get("Proxy-Authorization"))
			}
		}
		if r.Host == "chatgpt.com:443" {
			f.usageConnects.Add(1)
		}
		if r.Host == "auth.openai.com:443" {
			f.oauthConnects.Add(1)
		}
		remote, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			http.Error(w, "loopback unavailable", 502)
			return
		}
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			remote.Close()
			t.Error(err)
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
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.Proxy = nil
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: upstream.Certificate().DNSNames[0]}
	// No destination except the explicit loopback proxy may be dialed.
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if f.allowDirect && addr == "chatgpt.com:443" {
			f.usageDirect.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		}
		if f.allowDirect && addr == "api.kilo.ai:443" {
			f.deviceDirect.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		}
		if f.allowDirect && addr == "ai-gateway.kilo.ai:443" {
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		}
		if addr != proxy.Listener.Addr().String() {
			return nil, fmt.Errorf("non-loopback proxy dial blocked: %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original; transport.CloseIdleConnections() })
	cfg := &config.Config{CommercialMode: true, AuthDir: t.TempDir(), Kilo: config.KiloConfig{StateDir: t.TempDir(), ProxyLimit: 2, Proxies: []config.KiloProxy{{ID: "local-proxy", URL: proxy.URL}}}}
	cfg.SDKConfig.APIKeys = []string{"synthetic-kilo-key", "synthetic-codex-key", "synthetic-bps-key", "synthetic-legacy-key"}
	cfg.SDKConfig.ClientKeyScopes = []config.ClientKeyScope{{APIKey: "synthetic-kilo-key", Channel: "kilo"}, {APIKey: "synthetic-codex-key", Channel: "codex"}, {APIKey: "synthetic-bps-key", Channel: "bps"}}
	f.cfg = cfg
	cfg.RequestRetry = 2
	cfg.MaxRetryCredentials = 2
	f.manager = coreauth.NewManager(nil, nil, f.hook)
	f.manager.SetConfig(cfg)
	service := &Service{cfg: cfg, coreManager: f.manager}
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(cfg.AuthDir)
	f.manager.SetStore(store)
	if len(empty) == 0 || !empty[0] {
		for _, suffix := range []string{"a", "b"} {
			a, err := f.manager.Register(context.Background(), &coreauth.Auth{ID: t.Name() + "-" + suffix, Provider: "kilo", Status: coreauth.StatusActive, Metadata: map[string]any{"api_key": "synthetic-kilo-token", coreauth.KiloMachineIDKey: uuid.NewString()}})
			if err != nil {
				t.Fatal(err)
			}
			service.registerExecutorForAuth(a, false)
			if !service.registerModelsForAuth(context.Background(), a) {
				t.Fatal("model publication fence rejected live credential")
			}
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
		}
		registered, ok := f.manager.Executor("kilo")
		if !ok {
			t.Fatal("native registration missing")
		}
		f.executor, ok = registered.(*native.KiloExecutor)
		if !ok {
			t.Fatalf("registered executor=%T", registered)
		}
	}
	f.configPath = filepath.Join(t.TempDir(), "config.yaml")
	server := api.NewServer(cfg, f.manager, access.NewManager(), f.configPath, api.WithLocalManagementPassword("synthetic-management-key"), api.WithPostAuthPersistHook(func(ctx context.Context, a *coreauth.Auth) error {
		if f.postPersistGate != nil {
			f.postPersistGate(ctx, a)
		}
		return service.runtimeAuthSyncHook()(ctx, a)
	}))
	f.server = httptest.NewServer(server.Handler())
	t.Cleanup(f.server.Close)
	// The inbound test client uses its own ordinary loopback transport.
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(func() { f.server.Client().CloseIdleConnections() })
	return f
}

const kiloTerminal = `{"type":"response.completed","response":{"id":"resp-kilo","object":"response","created_at":1,"status":"completed","model":"openai/gpt-6.1-sol","output":[{"type":"message","id":"msg-kilo","role":"assistant","status":"completed","content":[{"type":"output_text","text":"kilo-ok","annotations":[]}]}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5},"unknown_terminal":{"preserved":true}}}`

func kiloSuccessStream(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{
		`{"type":"response.created","response":{"id":"resp-kilo","model":"openai/gpt-6.1-sol","created_at":1,"status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg-kilo","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.content_part.added","item_id":"msg-kilo","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		`{"type":"response.output_text.delta","item_id":"msg-kilo","output_index":0,"content_index":0,"delta":"kilo-ok"}`,
		kiloTerminal,
	} {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ojson.Get(event, "type").String(), event)
		w.(http.Flusher).Flush()
	}
}
func (f *kiloNativeFixture) request(t *testing.T, method, path, key, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
func (f *kiloNativeFixture) assertReleased(t *testing.T, wantResults int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ready := len(f.hook.snapshot()) == wantResults
		for _, p := range f.executor.KiloProxySnapshot().Proxies {
			ready = ready && p.Active == 0 && p.Owner == ""
		}
		for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
			ready = ready && a.Active == 0
		}
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("leases/results not released: pool=%+v results=%+v accounts=%+v", f.executor.KiloProxySnapshot(), f.hook.snapshot(), f.manager.CredentialConcurrencySnapshot())
}

func TestKiloNativeFourHTTPCombinationsThroughCONNECT(t *testing.T) {
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", protocol, stream), func(t *testing.T) {
				var sequence atomic.Int32
				seedUpstream := kiloSessionUpstream(t, make(chan kiloSessionSeen, 1))
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if protocol == "responses" && sequence.Add(1) == 1 {
						seedUpstream(w, r)
						return
					}
					kiloSuccessStream(w, r)
				})
				wantPosts := int32(1)
				if protocol == "responses" {
					code, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"register source tool"}`)
					if code != 200 {
						t.Fatalf("registered source call=%d %s", code, data)
					}
					<-f.requests
					f.assertReleased(t, 1)
					wantPosts = 2
				}
				input := `"input":[{"role":"user","content":[{"type":"input_text","text":"synthetic"},{"type":"input_image","image_url":"data:image/png;base64,c3ludGhldGlj"}]},{"type":"function_call","call_id":"call-resp-chain-1","name":"source_tool","arguments":"{\"exact\":1}"},{"type":"function_call_output","call_id":"call-resp-chain-1","output":"ok"}],"tools":[{"type":"function","name":"source_tool","parameters":{"type":"object"}},{"type":"custom","name":"source_custom","format":{"type":"text"}}],"unknown_source":{"keep":true}`
				if protocol == "chat/completions" {
					input = `"messages":[{"role":"user","content":"synthetic"}],"tools":[{"type":"function","function":{"name":"source_tool","parameters":{"type":"object"}}}]`
				}
				status, data := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":%t,%s}`, stream, input))
				if status != 200 || !bytes.Contains(data, []byte("kilo-ok")) {
					t.Fatalf("response status=%d body=%s", status, data)
				}
				f.assertReleased(t, int(wantPosts))
				if f.posts.Load() != wantPosts || (f.connects.Load() < 1 || f.connects.Load() > wantPosts) {
					t.Fatalf("POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
				}
				if !f.hook.snapshot()[0].Success {
					t.Fatalf("result=%+v", f.hook.snapshot())
				}
				upstream := <-f.requests
				if ojson.GetBytes(upstream, "model").String() != "openai/gpt-6.1-sol" || !ojson.GetBytes(upstream, "stream").Bool() {
					t.Fatalf("wire payload=%s", upstream)
				}
				if protocol == "responses" {
					for _, field := range []string{"input", "tools", "unknown_source"} {
						if ojson.GetBytes(upstream, field).Raw != ojson.Get(fmt.Sprintf("{%s}", input), field).Raw {
							t.Errorf("source field changed: %s body=%s", field, upstream)
						}
					}
					if !bytes.Contains(data, []byte("unknown_terminal")) {
						t.Error("unknown terminal field lost")
					}
				}
				t.Logf("production HTTP %s stream=%t POST=%d CONNECT=%d OnResult=%d account/proxy active=0", protocol, stream, wantPosts, f.connects.Load(), wantPosts)
			})
		}
	}
}

func TestKiloNativeFailuresDoNotReplay(t *testing.T) {
	for _, mode := range []string{"401", "502", "failed", "incomplete", "truncated"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if mode == "401" {
						http.Error(w, "synthetic-private-upstream-detail", 401)
						return
					}
					if mode == "502" {
						http.Error(w, "synthetic-private-upstream-detail", 502)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\"}}\n\n")
					w.(http.Flusher).Flush()
					if mode != "truncated" {
						_, _ = fmt.Fprintf(w, `data: {"type":"response.%s","response":{"status":"%s","error":{"message":"synthetic-private-upstream-detail"}}}`+"\n\n", mode, mode)
					}
				})
				status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","stream":%t}`, stream))
				if (!stream && status < 400) || bytes.Contains(data, []byte("synthetic-private-upstream-detail")) {
					t.Fatalf("failed response=%d %s", status, data)
				}
				wantStatus := 502
				if mode == "401" {
					wantStatus = 401
				}
				if stream && mode != "401" && mode != "502" {
					wantStatus = 200
				}
				if status != wantStatus {
					t.Fatalf("failure classification=%d want=%d body=%s", status, wantStatus, data)
				}
				if stream && wantStatus == 200 {
					terminalError := false
					scanner := bufio.NewScanner(bytes.NewReader(data))
					for scanner.Scan() {
						line := scanner.Text()
						if strings.HasPrefix(line, "data:") {
							event := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
							kind := ojson.Get(event, "type").String()
							if kind == "response.completed" {
								t.Fatalf("failure stream contained success: %s", data)
							}
							terminalError = terminalError || kind == "error" || kind == "response.failed" || ojson.Get(event, "error").Exists()
						}
					}
					if !terminalError {
						t.Fatalf("HTTP200 missing explicit failure terminal: %s", data)
					}
				}
				f.assertReleased(t, 1)
				if f.posts.Load() != 1 || f.connects.Load() != 1 || f.hook.snapshot()[0].Success {
					t.Fatalf("POST=%d CONNECT=%d result=%+v", f.posts.Load(), f.connects.Load(), f.hook.snapshot())
				}
				t.Logf("production failure %s stream=%t HTTP=%d POST=1 OnResult=1 released", mode, stream, status)
			})
		}
	}
}

func TestKiloNativeManagementAndRejectedOperations(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	for _, endpoint := range []string{"accounts", "status", "capabilities"} {
		path := "/v8/management/kilo/" + endpoint
		status, data := f.request(t, "GET", path, "synthetic-management-key", "")
		if status != 200 {
			t.Fatalf("%s: %d %s", path, status, data)
		}
		for _, secret := range []string{"synthetic-kilo-token", "local-proxy", "http://127.0.0.1", "connection_ref"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("DTO leaked %s: %s", secret, data)
			}
		}
		status, _ = f.request(t, "GET", path, "synthetic-kilo-key", "")
		if status != 401 {
			t.Fatalf("client key granted management: %d", status)
		}
	}
	status, data := f.request(t, "GET", "/v1/models", "synthetic-kilo-key", "")
	if status != 200 || len(ojson.GetBytes(data, "data").Array()) != len(helps.KiloModels) {
		t.Fatalf("native catalog=%d %s", status, data)
	}
	for _, key := range []string{"synthetic-legacy-key", "synthetic-codex-key"} {
		status, _ = f.request(t, "POST", "/v1/responses", key, `{"model":"gpt-6.1-sol","input":"test"}`)
		if status != 403 {
			t.Fatalf("cross channel status=%d", status)
		}
	}
	status, data = f.request(t, "POST", "/v1/messages/count_tokens", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"test"}]}`)
	if status != 501 {
		t.Fatalf("count status=%d %s", status, data)
	}
	status, data = f.request(t, "GET", "/v1/responses", "synthetic-kilo-key", "")
	if status != 501 {
		t.Fatalf("WS status=%d %s", status, data)
	}
	if f.posts.Load() != 0 || f.connects.Load() != 0 {
		t.Fatalf("unsupported operation reached upstream POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
	}
}

func TestKiloNativeCancellationAndLiveManagement(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			started := make(chan struct{})
			stopped := make(chan struct{})
			f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				close(started)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\"}}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(stopped)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-6.1-sol","input":"test","stream":%t}`, stream)))
			req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
			req.Header.Set("Content-Type", "application/json")
			done := make(chan struct{})
			go func() {
				defer close(done)
				response, err := f.server.Client().Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream did not start")
			}
			status, data := f.request(t, "GET", "/v8/management/kilo/status", "synthetic-management-key", "")
			if status != 200 || ojson.GetBytes(data, "busy_proxy_count").Int() != 1 || ojson.GetBytes(data, "healthy_proxy_count").Int() != 1 {
				t.Fatalf("live proxy snapshot=%d %s", status, data)
			}
			status, data = f.request(t, "GET", "/v8/management/kilo/accounts", "synthetic-management-key", "")
			active := int64(0)
			for _, a := range ojson.GetBytes(data, "accounts").Array() {
				active += a.Get("active").Int()
			}
			if status != 200 || active != 1 {
				t.Fatalf("live account snapshot=%d %s", status, data)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("inbound cancel blocked")
			}
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream connection survived cancel")
			}
			f.assertReleased(t, 1)
			for _, a := range f.manager.List() {
				if a.Disabled || a.Unavailable || a.Status != coreauth.StatusActive || !a.NextRetryAfter.IsZero() {
					t.Fatalf("cancel penalized account: %+v", a)
				}
			}
			if f.posts.Load() != 1 || f.connects.Load() != 1 {
				t.Fatalf("cancel replayed POST=%d", f.posts.Load())
			}
			t.Log("cancel: POST1 OnResult1, live DTO active/busy=1, released=0, account neutral")
		})
	}
}

func TestKiloNativeManagedConfigCannotOverridePool(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test"}`)
	if status != 200 {
		t.Fatalf("initial=%d %s", status, data)
	}
	next := f.cfg.CloneForRuntime()
	next.Kilo.ProxyLimit = 3
	next.Kilo.Proxies = nil
	f.executor.UpdateConfig(next)
	if f.executor.KiloProxySnapshot().Limit != 2 || len(f.executor.KiloProxySnapshot().Proxies) != 1 {
		t.Fatal("bootstrap config overwrote managed state")
	}
	t.Log("managed state preserves limit2 and stable proxy despite bootstrap config edits")
}

func TestKiloNativeUnverifiedProbeReleasesAccountAndNeverDirects(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	f := newKiloNativeFixture(t, kiloSuccessStream)
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		_, _ = io.WriteString(w, "not-an-ip")
	}
	if f.executor.KiloProxySnapshot().Proxies[0].Healthy {
		t.Fatal("fresh proxy falsely healthy")
	}
	done := make(chan struct{})
	var status int
	go func() {
		defer close(done)
		status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test"}`)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	for _, a := range f.manager.CredentialConcurrencySnapshot()["credentials"].([]coreauth.CredentialConcurrencyEntry) {
		if a.Active != 0 {
			t.Fatal("network health wait held account slot")
		}
	}
	close(finish)
	<-done
	if status != 503 || f.posts.Load() != 0 || f.connects.Load() != 0 {
		t.Fatalf("failed health status=%d POST=%d", status, f.posts.Load())
	}
	next := f.cfg.CloneForRuntime()
	next.Kilo.ProxyLimit = 5
	f.executor.UpdateConfig(next)
	a := f.manager.List()[0]
	a.Metadata["allow_direct_when_proxy_full"] = true
	_, release, wait, err := f.executor.TryAcquireEgress(a)
	if err == nil || wait || release != nil {
		t.Fatalf("failed proxy revived/direct admitted: wait=%t err=%v", wait, err)
	}
	if f.probes.Load() != 1 || f.executor.KiloProxySnapshot().Proxies[0].Healthy {
		t.Fatal("configuration edit reclassified failed health")
	}
	t.Log("unverified health=0; real proxy IP probe failed; account active=0 during wait; direct closed after config edit")
}

func TestKiloNativeConnectionFailureSurvivesConfigEdit(t *testing.T) {
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	})
	status, _ := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test"}`)
	if status != 502 {
		t.Fatalf("connection failure status=%d", status)
	}
	f.assertReleased(t, 1)
	if f.executor.KiloProxySnapshot().Proxies[0].Healthy {
		t.Fatal("failed connection still healthy")
	}
	next := f.cfg.CloneForRuntime()
	next.Kilo.ProxyLimit = 4
	f.executor.UpdateConfig(next)
	a := f.manager.List()[0]
	a.Metadata["allow_direct_when_proxy_full"] = true
	_, release, wait, err := f.executor.TryAcquireEgress(a)
	if err == nil || wait || release != nil {
		t.Fatal("failed connection revived/direct admitted after config edit")
	}
	if f.posts.Load() != 1 || f.probes.Load() != 1 {
		t.Fatalf("unexpected retry/reprobe POST=%d probes=%d", f.posts.Load(), f.probes.Load())
	}
}

func TestKiloNativePayloadRulesRemainFinal(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	next := f.cfg.CloneForRuntime()
	next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"model": "user-final-model", "reasoning.effort": "low", "unknown_source": "final"}}}
	f.executor.UpdateConfig(next)
	status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test"}`)
	if status != 200 {
		t.Fatalf("payload request=%d %s", status, data)
	}
	wire := <-f.requests
	if ojson.GetBytes(wire, "model").String() != "user-final-model" || ojson.GetBytes(wire, "reasoning.effort").String() != "low" || ojson.GetBytes(wire, "unknown_source").String() != "final" {
		t.Fatalf("post-finalizer mutation: %s", wire)
	}
	next = next.CloneForRuntime()
	next.Payload.Override[0].Params = map[string]any{"stream": false}
	f.executor.UpdateConfig(next)
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","input":"test"}`)
	if status != 400 || f.posts.Load() != 1 {
		t.Fatalf("user final stream overwritten status=%d POST=%d", status, f.posts.Load())
	}
}
