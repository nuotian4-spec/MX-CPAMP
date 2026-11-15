package management

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type deviceFlowFixture struct {
	h      *Handler
	router *gin.Engine
	clock  atomic.Int64
	polls  atomic.Int32
	poll   func(http.ResponseWriter, *http.Request)
}

func newDeviceFlowFixture(t *testing.T) *deviceFlowFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &deviceFlowFixture{}
	f.clock.Store(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC).UnixNano())
	oldStore := oauthSessions
	oauthSessions = newOAuthSessionStore(oauthSessionTTL)
	oauthSessions.kiloClock = func() time.Time { return time.Unix(0, f.clock.Load()) }
	t.Cleanup(func() { oauthSessions = oldStore })
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"code":"private-local-code","user_code":"LOCAL","verificationUrl":"https://kilo.ai/device","expiresIn":600}`)
			return
		}
		f.polls.Add(1)
		if f.poll != nil {
			f.poll(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"token":"synthetic-private-token","userEmail":"local@example.invalid"}`)
	}))
	t.Cleanup(upstream.Close)
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: upstream.Certificate().DNSNames[0]}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.kilo.ai:443" {
			return nil, errors.New("non-device dial blocked")
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original; transport.CloseIdleConnections() })
	cfg := &config.Config{AuthDir: t.TempDir(), Kilo: config.KiloConfig{StateDir: t.TempDir()}}
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(cfg.AuthDir)
	manager := coreauth.NewManager(store, nil, nil)
	manager.SetConfig(cfg)
	f.h = &Handler{cfg: cfg, authManager: manager}
	f.router = gin.New()
	f.router.POST("/start", f.h.StartKiloDevice)
	f.router.POST("/:id/poll", f.h.PollKiloDevice)
	f.router.POST("/:id/save", f.h.SaveKiloDevice)
	f.router.DELETE("/:id", f.h.CancelKiloDevice)
	return f
}
func (f *deviceFlowFixture) request(method, path string) *httptest.ResponseRecorder {
	data := "{}"
	if path == "/start" {
		data = `{"name":"local","authorization_egress":{"mode":"direct"}}`
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(data)))
	return w
}
func (f *deviceFlowFixture) start(t *testing.T) (string, *kiloDeviceSession) {
	t.Helper()
	w := f.request("POST", "/start")
	if w.Code != 200 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	id := ojson.Get(w.Body.String(), "id").String()
	return id, oauthSessions.kiloSession(id, f.h)
}
func deviceStatus(t *testing.T, w *httptest.ResponseRecorder, status int, state string) {
	t.Helper()
	if w.Code != status || (state != "" && ojson.Get(w.Body.String(), "status").String() != state) {
		t.Fatalf("status=%d body=%s want=%d/%s", w.Code, w.Body.String(), status, state)
	}
	if strings.Contains(w.Body.String(), "private-local-code") || strings.Contains(w.Body.String(), "synthetic-private-token") {
		t.Fatal("session secret leaked")
	}
}
func TestKiloDevicePollClockAndFixedApproval(t *testing.T) {
	f := newDeviceFlowFixture(t)
	f.poll = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) }
	id, flow := f.start(t)
	deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "pending")
	f.clock.Add(int64(2999 * time.Millisecond))
	deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "pending")
	if f.polls.Load() != 1 {
		t.Fatal("3s gate bypassed")
	}
	f.clock.Add(int64(time.Millisecond))
	f.poll = nil
	deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "approved")
	deadline := flow.expires
	f.clock.Add(int64(299 * time.Second))
	w := f.request("POST", "/"+id+"/poll")
	deviceStatus(t, w, 200, "approved")
	if ojson.Get(w.Body.String(), "save_expires_in").Int() != 1 || !flow.expires.Equal(deadline) || f.polls.Load() != 2 {
		t.Fatal("approved poll renewed deadline or called upstream")
	}
	deviceStatus(t, f.request("POST", "/"+id+"/save"), 201, "")
	if flow.state != "saved" || flow.code != "" || flow.token != "" {
		t.Fatal("save did not clear session secrets")
	}
}
func TestKiloDeviceExactExpiryAndTransientFailures(t *testing.T) {
	for _, kind := range []string{"approved-300", "pending-600", "denied", "expired", "rate", "malformed", "bad-token", "oversize", "network"} {
		t.Run(kind, func(t *testing.T) {
			f := newDeviceFlowFixture(t)
			id, flow := f.start(t)
			original := flow.expires
			switch kind {
			case "approved-300":
				deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "approved")
				f.clock.Add(int64(300 * time.Second))
				deviceStatus(t, f.request("POST", "/"+id+"/save"), 409, "")
			case "pending-600":
				f.clock.Add(int64(600 * time.Second))
				deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "expired")
			default:
				want := 502
				state := ""
				f.poll = func(w http.ResponseWriter, r *http.Request) {
					switch kind {
					case "denied":
						w.WriteHeader(403)
					case "expired":
						w.WriteHeader(410)
					case "rate":
						w.WriteHeader(429)
					case "malformed":
						_, _ = io.WriteString(w, "private-local-code")
					case "bad-token":
						_, _ = io.WriteString(w, `{"token":"bad token","userEmail":"local"}`)
					case "oversize":
						_, _ = io.WriteString(w, strings.Repeat("x", 65537))
					case "network":
						c, _, _ := w.(http.Hijacker).Hijack()
						_ = c.Close()
					}
				}
				if kind == "denied" || kind == "expired" {
					want = 200
					state = kind
				}
				if kind == "rate" {
					want = 429
				}
				if kind == "network" {
					want = 503
				}
				deviceStatus(t, f.request("POST", "/"+id+"/poll"), want, state)
				if kind != "denied" && kind != "expired" {
					if flow.state != "pending" || !flow.expires.Equal(original) || flow.code == "" {
						t.Fatal("retryable failure consumed session")
					}
					return
				}
			}
			if flow.code != "" || flow.token != "" || flow.ctx.Err() == nil {
				t.Fatal("terminal session retained secrets")
			}
		})
	}
}
func TestKiloDeviceConcurrentPollAndCancellation(t *testing.T) {
	for _, action := range []string{"cancel", "expire", "approved"} {
		t.Run(action, func(t *testing.T) {
			f := newDeviceFlowFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			f.poll = func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				_, _ = io.WriteString(w, `{"token":"synthetic-private-token","userEmail":"local"}`)
			}
			id, flow := f.start(t)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.request("POST", "/"+id+"/poll") }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("poll not entered")
			}
			f.clock.Add(int64(4 * time.Second))
			deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "pending")
			if f.polls.Load() != 1 {
				t.Fatal("concurrent poll sent twice")
			}
			want := "approved"
			if action == "cancel" {
				deviceStatus(t, f.request("DELETE", "/"+id), 200, "cancelled")
				want = "cancelled"
			}
			if action == "expire" {
				f.clock.Add(int64(600 * time.Second))
				deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "expired")
				want = "expired"
			}
			once.Do(func() { close(release) })
			select {
			case w := <-done:
				deviceStatus(t, w, 200, want)
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled HTTP did not return")
			}
			if flow.state != want || (want != "approved" && flow.token != "") {
				t.Fatal("late approval resurrected terminal session")
			}
		})
	}
}

type deviceClaimStore struct {
	coreauth.Store
	entered, release chan struct{}
	fail             bool
}

func (s *deviceClaimStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	close(s.entered)
	<-s.release
	if s.fail {
		return "", errors.New("synthetic-private-token")
	}
	return s.Store.Save(ctx, a)
}
func TestKiloDeviceSaveClaimAndStableRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "retry"}[fail], func(t *testing.T) {
			f := newDeviceFlowFixture(t)
			id, flow := f.start(t)
			deviceStatus(t, f.request("POST", "/"+id+"/poll"), 200, "approved")
			reserved, deadline := flow.accountID, flow.expires
			base := sdkauth.NewFileTokenStore()
			base.SetBaseDir(f.h.cfg.AuthDir)
			store := &deviceClaimStore{Store: base, entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
			f.h.authManager.SetStore(store)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.request("POST", "/"+id+"/save") }()
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("save not entered")
			}
			deviceStatus(t, f.request("DELETE", "/"+id), 409, "")
			deviceStatus(t, f.request("POST", "/"+id+"/save"), 409, "")
			close(store.release)
			want := 201
			if fail {
				want = 503
			}
			deviceStatus(t, <-done, want, "")
			if fail {
				if flow.state != "approved" || !flow.expires.Equal(deadline) || flow.token == "" || len(f.h.authManager.List()) != 0 {
					t.Fatal("failed save lost retry state")
				}
				f.h.authManager.SetStore(base)
				deviceStatus(t, f.request("POST", "/"+id+"/save"), 201, "")
			}
			if flow.accountID != reserved || len(f.h.authManager.List()) != 1 {
				t.Fatal("retry changed reserved identity")
			}
			deviceStatus(t, f.request("POST", "/"+id+"/save"), 201, "")
			deviceStatus(t, f.request("DELETE", "/"+id), 200, "saved")
			if _, ok := oauthSessions.Get(id); ok {
				t.Fatal("generic OAuth projection exposed typed device session")
			}
		})
	}
}

func TestKiloDeviceInboundCancellationRemainsRetryable(t *testing.T) {
	f := newDeviceFlowFixture(t)
	id, flow := f.start(t)
	deadline := flow.expires
	entered, closed := make(chan struct{}), make(chan struct{})
	f.poll = func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(closed) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/"+id+"/poll", strings.NewReader("{}")).WithContext(ctx)
		f.router.ServeHTTP(w, req)
		done <- w
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("poll not entered")
	}
	cancel()
	select {
	case w := <-done:
		deviceStatus(t, w, 499, "")
	case <-time.After(5 * time.Second):
		t.Fatal("request cancellation did not return")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream socket survived cancellation")
	}
	if flow.state != "pending" || !flow.expires.Equal(deadline) || flow.code == "" {
		t.Fatal("request cancellation consumed session")
	}
}

func TestUX1DeviceExpiryReleasesPinWithoutPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan struct{}, 1)
	flow := &kiloDeviceSession{ctx: ctx, cancel: cancel, state: "pending", code: "synthetic", expires: time.Now().Add(20 * time.Millisecond), releasePin: func() { released <- struct{}{} }}
	flow.armExpiryLocked(time.Now(), time.Now)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("expiry kept pin without another request")
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.state != "expired" || flow.code != "" || flow.ctx.Err() == nil {
		t.Fatal("expiry did not clear session")
	}
}
