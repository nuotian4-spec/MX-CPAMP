package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"golang.org/x/crypto/bcrypt"
)

type bpsAccessTransport func(*http.Request) (*http.Response, error)

func (f bpsAccessTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type bpsAccessStore struct{ writes atomic.Int32 }

func (*bpsAccessStore) List(context.Context) ([]*coreauth.Auth, error) { return nil, nil }
func (s *bpsAccessStore) Save(context.Context, *coreauth.Auth) (string, error) {
	s.writes.Add(1)
	return "", errors.New("unexpected credential save")
}
func (s *bpsAccessStore) Delete(context.Context, string) error {
	s.writes.Add(1)
	return errors.New("unexpected credential delete")
}

func newBPSAccessServer(t *testing.T) (*Server, *coreauth.Manager, *coreauth.Auth, *bpsAccessStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := &bpsAccessStore{}
	manager := coreauth.NewManager(store, nil, nil)
	var selected *coreauth.Auth
	for _, item := range []*coreauth.Auth{
		{ID: "bps-a.json", Provider: "bps", Metadata: map[string]any{"access_token": "TOKEN_A", "account_id": "ACCOUNT_A", "refresh_token": "NEVER_REFRESH_A", "sub": "SUB_A"}},
		{ID: "bps-b.json", Provider: "bps", Metadata: map[string]any{"access_token": "TOKEN_B", "account_id": "ACCOUNT_B"}},
		{ID: "codex.json", Provider: "codex", Metadata: map[string]any{"access_token": "CODEX_TOKEN"}},
	} {
		auth, err := manager.Register(coreauth.WithSkipPersist(context.Background()), item)
		if err != nil {
			t.Fatal(err)
		}
		if item.ID == "bps-a.json" {
			selected = auth
		}
	}
	cfg := &config.Config{AuthDir: t.TempDir()}
	secret, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RemoteManagement.SecretKey = string(secret)
	handler := management.NewHandler(cfg, "", manager)
	handler.SetLocalPassword("test-password")
	server := &Server{cfg: cfg, engine: gin.New(), mgmt: handler}
	server.managementRoutesEnabled.Store(true)
	server.registerManagementV8Routes()
	return server, manager, selected, store
}

func callBPSAccess(server *Server, ctx context.Context, query string, authorized bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v8/management/bps/credentials/access"+query, nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1234"
	if authorized {
		req.Header.Set("Authorization", "Bearer test-password")
	}
	w := httptest.NewRecorder()
	server.engine.ServeHTTP(w, req)
	return w
}

func TestBPSAccessPublicHandlerReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantModels, wantError string
		status                            int
		allowed                           bool
	}{
		{"allowed and filtered", `{"allowed":true,"model_catalog":{"models":[{"id":"gpt-6-luna"},{"id":"gpt-6-luna"},{"id":"gpt-restricted"},{"id":"gpt-unavailable","available":false},{"id":"gpt-disabled","enabled":false},{"id":"gpt-picker","model_picker_enabled":false},{"id":"gpt-policy","policy":{"state":"disabled"}},{"id":"gpt-unconfigured","policy":{"state":"unconfigured"}},{"id":"other-model"}],"restricted_models":["gpt-restricted"]}}`, `["gpt-6-luna"]`, "", 200, true},
		{"denied", `{"allowed":false}`, `[]`, "", 200, false},
		{"empty", `{"allowed":true,"model_catalog":{"models":[]}}`, `[]`, "", 200, true},
		{"unauthorized", `PRIVATE_UPSTREAM_BODY`, "null", "unauthorized", 401, false},
		{"forbidden", `PRIVATE_UPSTREAM_BODY`, "null", "forbidden", 403, false},
		{"rate limited", `PRIVATE_UPSTREAM_BODY`, "null", "rate_limited", 429, false},
		{"upstream failed", `PRIVATE_UPSTREAM_BODY`, "null", "upstream_error", 503, false},
		{"redirect", `PRIVATE_UPSTREAM_BODY`, "null", "redirect_rejected", 302, false},
		{"invalid json", `PRIVATE_UPSTREAM_BODY`, "null", "invalid_response", 200, false},
		{"missing allowed", `{}`, "null", "invalid_response", 200, false},
		{"wrong allowed type", `{"allowed":"true"}`, "null", "invalid_response", 200, false},
		{"missing catalog", `{"allowed":true}`, "null", "invalid_response", 200, false},
		{"bad catalog", `{"allowed":true,"model_catalog":{"models":{}}}`, "null", "invalid_response", 200, false},
		{"bad optional flag", `{"allowed":true,"model_catalog":{"models":[{"id":"gpt-valid","available":"yes"}]}}`, "null", "invalid_response", 200, false},
		{"bad policy", `{"allowed":true,"model_catalog":{"models":[{"id":"gpt-valid","policy":{"state":123}}]}}`, "null", "invalid_response", 200, false},
		{"oversize", strings.Repeat(" ", (2<<20)+1), "null", "invalid_response", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, manager, selected, store := newBPSAccessServer(t)
			before := selected.Clone()
			calls := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != helps.BPSAccessURL || r.Body != nil {
					t.Fatalf("unexpected upstream operation: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer TOKEN_A" || r.Header.Get("chatgpt-account-id") != "ACCOUNT_A" || r.Header.Get("x-openai-account-id") != "ACCOUNT_A" || r.Header.Get("Accept") != "application/json" || r.Header.Get("x-basispoints-auth-mode") != "chatgpt" || r.Header.Get("x-openai-internal-basispoints-client-agent-profile") != "excel" {
					t.Fatal("selected BPS headers were not preserved")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
					t.Fatal("missing five-second management budget")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://untrusted.invalid/redirect"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			w := callBPSAccess(server, ctx, "?auth_index="+selected.EnsureIndex()+"&auth_id="+url.QueryEscape(selected.ID), true)
			root := ojson.ParseBytes(w.Body.Bytes())
			if w.Code != 200 || root.Get("provider").String() != "bps" || root.Get("auth_index").String() != selected.Index || root.Get("auth_id").String() != selected.ID || root.Get("models").Raw != tc.wantModels {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if tc.wantError == "" {
				if !root.Get("allowed").IsBool() || root.Get("allowed").Bool() != tc.allowed || root.Get("error").Raw != "null" {
					t.Fatal(w.Body.String())
				}
			} else if root.Get("allowed").Raw != "null" || root.Get("error").String() != tc.wantError {
				t.Fatal(w.Body.String())
			}
			if root.Get("upstream_status").Int() != int64(tc.status) || root.Get("quota").Raw != "null" || root.Get("inference_verified").Bool() || root.Get("auto_recover_eligible").Bool() || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Body.String())
			}
			if _, err := time.Parse(time.RFC3339, root.Get("queried_at").String()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || store.writes.Load() != 0 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "TOKEN_") || strings.Contains(w.Body.String(), "ACCOUNT_") {
				t.Fatal("probe leaked upstream data or performed extra operations")
			}
			after, _ := manager.GetByID(selected.ID)
			if after.Generation != before.Generation || after.CredentialVersion != before.CredentialVersion || after.LastRefreshedAt != before.LastRefreshedAt || after.Metadata["access_token"] != before.Metadata["access_token"] || after.Disabled != before.Disabled {
				t.Fatal("probe changed credential state")
			}
		})
	}
}

func TestBPSAccessPublicHandlerRejectsReferences(t *testing.T) {
	server, manager, selected, store := newBPSAccessServer(t)
	calls := 0
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected upstream call")
	}))
	codex, _ := manager.GetByID("codex.json")
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 400}, {"?auth_index=", 400}, {"?auth_index=unknown", 404},
		{"?auth_index=" + codex.EnsureIndex(), 400},
		{"?auth_index=" + selected.Index + "&auth_id=bps-b.json", 409},
		{"?auth_index=" + selected.Index + "&url=https://untrusted.invalid", 400},
		{"?auth_index=" + selected.Index + "&auth_index=" + selected.Index, 400},
		{"?auth_index=%zz", 400},
	} {
		if w := callBPSAccess(server, ctx, tc.query, true); w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body.String())
		}
	}
	if w := callBPSAccess(server, ctx, "?auth_index="+selected.Index, false); w.Code != 401 {
		t.Fatalf("missing management authentication accepted: %d", w.Code)
	}
	w := httptest.NewRecorder()
	server.engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v0/management/bps/credentials/access", nil))
	if w.Code != 404 || calls != 0 || store.writes.Load() != 0 {
		t.Fatal("rejected reference dispatched or changed state")
	}
}

func TestBPSAccessPublicHandlerTransportAndSnapshot(t *testing.T) {
	for _, category := range []string{"timeout", "canceled", "transport_error", "credential_changed", "global_proxy_changed", "credential_incomplete"} {
		t.Run(category, func(t *testing.T) {
			server, manager, selected, store := newBPSAccessServer(t)
			if category == "credential_incomplete" {
				selected.Metadata["access_token"] = ""
				if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), selected); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch category {
				case "timeout":
					return nil, context.DeadlineExceeded
				case "canceled":
					return nil, context.Canceled
				case "transport_error":
					return nil, errors.New("PRIVATE_TOKEN_A")
				case "credential_changed":
					changed := selected.Clone()
					changed.Metadata["account_id"] = "ACCOUNT_CHANGED"
					if _, err := manager.Update(coreauth.WithSkipPersist(context.Background()), changed); err != nil {
						t.Fatal(err)
					}
				case "global_proxy_changed":
					changed := server.cfg.CloneForRuntime()
					changed.ProxyURL = "http://127.0.0.1:29999"
					server.mgmt.SetConfig(changed)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"allowed":true,"model_catalog":{"models":[]}}`))}, nil
			}))
			w := callBPSAccess(server, ctx, "?auth_index="+selected.Index, true)
			root := ojson.ParseBytes(w.Body.Bytes())
			wantError := category
			if category == "global_proxy_changed" {
				wantError = "credential_changed"
			}
			if w.Code != 200 || root.Get("error").String() != wantError || root.Get("allowed").Raw != "null" || root.Get("models").Raw != "null" || strings.Contains(w.Body.String(), "PRIVATE") || store.writes.Load() != 0 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if category == "credential_incomplete" && calls != 0 || category != "credential_incomplete" && calls != 1 {
				t.Fatal("unexpected request count", calls)
			}
		})
	}
}

type bpsAccessCancelBody struct {
	ctx    context.Context
	closed chan struct{}
	once   sync.Once
}

func (b *bpsAccessCancelBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *bpsAccessCancelBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestBPSAccessPublicHandlerParentCancellation(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			server, _, selected, store := newBPSAccessServer(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			closed := make(chan struct{})
			ctx := context.WithValue(parent, "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
				close(started)
				if stage == "headers" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &bpsAccessCancelBody{ctx: r.Context(), closed: closed}}, nil
			}))
			finished := make(chan *httptest.ResponseRecorder, 1)
			go func() { finished <- callBPSAccess(server, ctx, "?auth_index="+selected.Index, true) }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("probe did not start")
			}
			cancel()
			select {
			case w := <-finished:
				root := ojson.ParseBytes(w.Body.Bytes())
				if w.Code != 200 || root.Get("error").String() != "canceled" || root.Get("allowed").Raw != "null" || root.Get("models").Raw != "null" || store.writes.Load() != 0 {
					t.Fatal(w.Body.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("parent cancellation did not stop the probe")
			}
			if stage == "body" {
				select {
				case <-closed:
				default:
					t.Fatal("canceled response body was not closed")
				}
			}
		})
	}
}

func TestBPSAccessPublicHandlerKeepsShorterDeadline(t *testing.T) {
	server, _, selected, _ := newBPSAccessServer(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := parent.Deadline()
	ctx := context.WithValue(parent, "cliproxy.roundtripper", bpsAccessTransport(func(r *http.Request) (*http.Response, error) {
		if got, _ := r.Context().Deadline(); !got.Equal(want) {
			t.Error("probe replaced a shorter parent deadline")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"allowed":false}`))}, nil
	}))
	if w := callBPSAccess(server, ctx, "?auth_index="+selected.Index, true); w.Code != 200 || ojson.ParseBytes(w.Body.Bytes()).Get("error").Raw != "null" {
		t.Fatal(w.Body.String())
	}
}
