package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type resetTestTransport func(*http.Request) (*http.Response, error)

func (f resetTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newResetHandler(t *testing.T) (*Handler, *coreauth.Auth, *coreauth.Auth) {
	t.Helper()
	m := coreauth.NewManager(nil, nil, nil)
	auths := []*coreauth.Auth{}
	for _, id := range []string{"reset-a", "reset-a-copy"} {
		a, err := m.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: id, Provider: "bps", Metadata: map[string]any{"access_token": "SYNTHETIC_TOKEN", "account_id": "synthetic-account", "sub": "synthetic-sub"}, Attributes: map[string]string{"runtime_only": "true"}})
		if err != nil {
			t.Fatal(err)
		}
		auths = append(auths, a)
	}
	return &Handler{cfg: &config.Config{AuthDir: t.TempDir()}, authManager: m}, auths[0], auths[1]
}

func resetCall(h *Handler, a *coreauth.Auth, method, path string, body any, tr http.RoundTripper) *httptest.ResponseRecorder {
	values := url.Values{"auth_index": {a.EnsureIndex()}, "auth_id": {a.ID}}
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		path = parts[0]
		extra, _ := url.ParseQuery(parts[1])
		for k, v := range extra {
			values[k] = v
		}
	}
	doc := ojson.NewDocString("{}")
	if fields, ok := body.(map[string]any); ok {
		for key, value := range fields {
			_ = doc.Set(key, value)
		}
	}
	req := httptest.NewRequest(method, path+"?"+values.Encode(), strings.NewReader(doc.String()))
	req = req.WithContext(context.WithValue(req.Context(), "cliproxy.roundtripper", tr))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	switch {
	case path == "/quota":
		h.GetBPSCredentialQuota(c)
	case method == "GET":
		h.GetBPSCredentialReset(c)
	default:
		h.PostBPSCredentialReset(c)
	}
	return w
}

func resetInput(id, previous string) map[string]any {
	return map[string]any{"confirm": true, "redeem_request_id": id, "previous_reset_id": previous}
}

func TestBPSResetCASIdempotencyAndReadback(t *testing.T) {
	h, a, copyAuth := newResetHandler(t)
	var posts, gets atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_TOKEN" || r.Header.Get("Chatgpt-Account-Id") != "synthetic-account" {
			t.Fatal("身份头错误")
		}
		body := `{"available_count":2}`
		if r.Method == "POST" {
			posts.Add(1)
			raw, _ := io.ReadAll(r.Body)
			input := ojson.ParseBytes(raw).Map()
			if len(input) != 1 || !input["redeem_request_id"].Exists() {
				t.Fatal(input)
			}
			body = `{"result":"reset"}`
		} else {
			gets.Add(1)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	quota := resetCall(h, a, "GET", "/quota", nil, tr)
	if quota.Code != 200 || !strings.Contains(quota.Body.String(), `"available_count":2`) {
		t.Fatal(quota.Code, quota.Body.String())
	}
	id := uuid.NewString()
	first := resetCall(h, a, "POST", "/reset", resetInput(id, ""), tr)
	if first.Code != 200 || posts.Load() != 1 || !strings.Contains(first.Body.String(), `"status":"submitted"`) {
		t.Fatal(first.Code, first.Body.String(), posts.Load())
	}
	before := gets.Load()
	again := resetCall(h, a, "POST", "/reset", resetInput(id, ""), tr)
	if again.Code != 200 || posts.Load() != 1 || gets.Load() != before {
		t.Fatal("相同ID重复出站")
	}
	if conflict := resetCall(h, copyAuth, "POST", "/reset", resetInput(id, ""), tr); conflict.Code != 409 {
		t.Fatal("相同ID跨凭证未拒绝")
	}
	if duplicate := resetCall(h, copyAuth, "POST", "/reset", resetInput(uuid.NewString(), ""), tr); duplicate.Code != 409 || posts.Load() != 1 {
		t.Fatal("同账号复制凭证绕过CAS")
	}
	result := resetCall(h, a, "GET", "/reset?redeem_request_id="+id, nil, tr)
	if result.Code != 200 || gets.Load() != before {
		t.Fatal("结果回读触发上游请求")
	}
	updated := resetCall(h, a, "GET", "/quota", nil, tr)
	if !strings.Contains(updated.Body.String(), id) {
		t.Fatal(updated.Body.String())
	}
	if next := resetCall(h, a, "POST", "/reset", resetInput(uuid.NewString(), id), tr); next.Code != 200 || posts.Load() != 2 {
		t.Fatal(next.Code, next.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(h.cfg.AuthDir, "bps-reset-ledger.brs"))
	if strings.Contains(string(raw), "SYNTHETIC_TOKEN") {
		t.Fatal("持久状态包含令牌")
	}
}

func TestBPSResetConcurrentOldRevisionConsumesOnce(t *testing.T) {
	h, a, b := newResetHandler(t)
	var posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	})
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, auth := range []*coreauth.Auth{a, b} {
		wg.Add(1)
		go func(auth *coreauth.Auth) {
			defer wg.Done()
			statuses <- resetCall(h, auth, "POST", "/reset", resetInput(uuid.NewString(), ""), tr).Code
		}(auth)
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for code := range statuses {
		if code == 200 {
			ok++
		}
		if code == 409 {
			conflict++
		}
	}
	if posts.Load() != 1 || ok != 1 || conflict != 1 {
		t.Fatal(posts.Load(), ok, conflict)
	}
}

func TestBPSResetUnknownSurvivesHandlerRestart(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var calls, posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method == "POST" {
			posts.Add(1)
			return nil, errors.New("synthetic disconnect")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	})
	id := uuid.NewString()
	w := resetCall(h, a, "POST", "/reset", resetInput(id, ""), tr)
	if w.Code != 502 || posts.Load() != 1 || !strings.Contains(w.Body.String(), `"status":"unknown"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted := &Handler{cfg: h.cfg.CloneForRuntime(), authManager: h.authManager}
	before := calls.Load()
	if repeat := resetCall(restarted, a, "POST", "/reset", resetInput(id, ""), tr); repeat.Code != 502 {
		t.Fatal(repeat.Code)
	}
	if next := resetCall(restarted, a, "POST", "/reset", resetInput(uuid.NewString(), id), tr); next.Code != 409 {
		t.Fatal(next.Code)
	}
	if result := resetCall(restarted, a, "GET", "/reset?redeem_request_id="+id, nil, tr); result.Code != 200 || !strings.Contains(result.Body.String(), `"status":"unknown"`) {
		t.Fatal(result.Code, result.Body.String())
	}
	if calls.Load() != before || posts.Load() != 1 {
		t.Fatal("重启后重复消费", calls.Load(), posts.Load())
	}
}

func TestBPSResetPersistFailureAndMissingIdentityNeverConsume(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	})
	if err := os.Mkdir(filepath.Join(h.cfg.AuthDir, "bps-reset-ledger.brs"), 0700); err != nil {
		t.Fatal(err)
	}
	w := resetCall(h, a, "POST", "/reset", resetInput(uuid.NewString(), ""), tr)
	if w.Code != 503 || posts.Load() != 0 {
		t.Fatal(w.Code, posts.Load())
	}
	h2, a2, _ := newResetHandler(t)
	a2.Metadata["sub"] = ""
	h2.authManager.Update(coreauth.WithSkipPersist(context.Background()), a2)
	w = resetCall(h2, a2, "POST", "/reset", resetInput(uuid.NewString(), ""), tr)
	if w.Code != 400 || posts.Load() != 0 {
		t.Fatal(w.Code, posts.Load())
	}
}

func TestBPSResetPendingSurvivesRestartAndZeroCardsNeverConsume(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var calls, posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method == "POST" {
			posts.Add(1)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":0}`)), Header: http.Header{}}, nil
	})
	if w := resetCall(h, a, "POST", "/reset", resetInput(uuid.NewString(), ""), tr); w.Code != 409 || posts.Load() != 0 {
		t.Fatal(w.Code, posts.Load())
	}
	id := uuid.NewString()
	account := `["synthetic-account","synthetic-sub"]`
	ledger := bpsResetLedger{Records: map[string]bpsResetRecord{id: {Account: account, AuthID: a.ID, HTTPStatus: 409, Receipt: bpsResetReceipt{RequestID: id, Status: "pending"}}}, Last: map[string]string{account: id}}
	if err := saveBPSResetLedger(filepath.Join(h.cfg.AuthDir, "bps-reset-ledger.brs"), ledger); err != nil {
		t.Fatal(err)
	}
	restarted := &Handler{cfg: h.cfg.CloneForRuntime(), authManager: h.authManager}
	before := calls.Load()
	if w := resetCall(restarted, a, "POST", "/reset", resetInput(id, ""), tr); w.Code != 409 || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != before || posts.Load() != 0 {
		t.Fatal("pending触发消费")
	}
}

func TestBPSResetPendingWriteFailureNeverConsumes(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts.Add(1)
		} else {
			// Fail the atomic replacement after loading state; consume requires a durable pending record.
			if err := os.Mkdir(filepath.Join(h.cfg.AuthDir, "bps-reset-ledger.brs"), 0700); err != nil {
				t.Error(err)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	})
	w := resetCall(h, a, "POST", "/reset", resetInput(uuid.NewString(), ""), tr)
	if w.Code != 503 || posts.Load() != 0 {
		t.Fatal(w.Code, posts.Load())
	}
}

func TestBPSResetInvalidConfirmationNeverCallsUpstream(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var calls atomic.Int64
	tr := resetTestTransport(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("unexpected") })
	for _, input := range []map[string]any{
		{"confirm": false, "redeem_request_id": uuid.NewString(), "previous_reset_id": ""},
		{"confirm": true, "redeem_request_id": uuid.NewString()},
		{"confirm": true, "redeem_request_id": "invalid", "previous_reset_id": ""},
		{"confirm": true, "redeem_request_id": uuid.NewString(), "previous_reset_id": "", "extra": true},
	} {
		if w := resetCall(h, a, "POST", "/reset", input, tr); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}

func TestBPSResetQuotaFailurePreservesUnknownReadback(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var posts atomic.Int64
	tr := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts.Add(1)
			return nil, errors.New("disconnect")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"available_count":2}`)), Header: http.Header{}}, nil
	})
	id := uuid.NewString()
	if w := resetCall(h, a, "POST", "/reset", resetInput(id, ""), tr); w.Code != 502 {
		t.Fatal(w.Code)
	}
	restarted := &Handler{cfg: h.cfg.CloneForRuntime(), authManager: h.authManager}
	denied := resetTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
	})
	w := resetCall(restarted, a, "GET", "/quota", nil, denied)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available_count":null`) || !strings.Contains(w.Body.String(), `"query_error":"forbidden"`) || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), `"status":"unknown"`) || posts.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), posts.Load())
	}
}

func TestBPSResetQuotaFailureStillChecksIdentityAndConfiguration(t *testing.T) {
	for _, change := range []string{"account", "sub", "proxy", "auth_dir"} {
		t.Run(change, func(t *testing.T) {
			h, a, _ := newResetHandler(t)
			tr := resetTestTransport(func(*http.Request) (*http.Response, error) {
				if change == "account" || change == "sub" {
					key := "account_id"
					if change == "sub" {
						key = "sub"
					}
					current, _ := h.authManager.GetByID(a.ID)
					current.Metadata[key] = "changed"
					if _, err := h.authManager.Update(coreauth.WithSkipPersist(context.Background()), current); err != nil {
						t.Error(err)
					}
				} else {
					cfg := h.configSnapshot().CloneForRuntime()
					if change == "proxy" {
						cfg.ProxyURL = "http://127.0.0.1:1"
					} else {
						cfg.AuthDir = t.TempDir()
					}
					h.SetConfig(cfg)
				}
				return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
			})
			w := resetCall(h, a, "GET", "/quota", nil, tr)
			if w.Code != 409 || strings.Contains(w.Body.String(), "last_reset") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestBPSResetStrictSingleObjectAndSize(t *testing.T) {
	h, a, _ := newResetHandler(t)
	var calls atomic.Int64
	tr := resetTestTransport(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("unexpected") })
	for _, raw := range []string{`{} {}`, `{"confirm":"true","redeem_request_id":"11111111-1111-4111-8111-111111111111","previous_reset_id":""}`, `{"confirm":true,"redeem_request_id":"11111111-1111-4111-8111-111111111111","previous_reset_id":null}`, `{"confirm":true,"redeem_request_id":"11111111-1111-4111-8111-111111111111","previous_reset_id":"` + strings.Repeat("a", 4096) + `"}`} {
		req := httptest.NewRequest("POST", "/reset?auth_index="+a.EnsureIndex()+"&auth_id="+a.ID, strings.NewReader(raw))
		req = req.WithContext(context.WithValue(req.Context(), "cliproxy.roundtripper", tr))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		h.PostBPSCredentialReset(c)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}
