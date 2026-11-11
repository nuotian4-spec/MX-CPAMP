package oauthlogin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type resolver struct {
	mu    sync.Mutex
	setup store.Setup
}

func (r *resolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setup, true, nil
}
func (r *resolver) setURL(value string) { r.mu.Lock(); r.setup.CPAUpstreamURL = value; r.mu.Unlock() }
func jsonReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func waitTask(t *testing.T, s *Service, id string, predicate func(Task) bool) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, _ := s.Get(id)
		if predicate(v) {
			return v
		}
		time.Sleep(time.Millisecond)
	}
	v, _ := s.Get(id)
	t.Fatalf("任务未收敛: %+v", v)
	return v
}
func fakeCredentials(profile string) *credentials {
	if profile == "bps" {
		return &credentials{Profile: "excel", ClientID: "app_fnr0pYvVwwFDocDumLG3H2Bp", AccessToken: "ACCESS_SECRET", RefreshToken: "REFRESH_SECRET", AccountID: "a1", Email: "test@example.invalid", Expired: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	}
	return &credentials{Profile: "codex", ClientID: "app_EMoamEEZ73f0CkXaXp7hrann", AccessToken: "ACCESS_SECRET", RefreshToken: "REFRESH_SECRET", IDToken: "ID_SECRET", AccountID: "a1", Email: "test@example.invalid", Expired: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}
func TestTaskImportContract(t *testing.T) {
	for _, test := range []struct {
		name, profile, state string
		wrongClient, unsaved bool
		want                 string
	}{
		{name: "codex", profile: "codex", state: "succeeded", want: "succeeded"},
		{name: "bps", profile: "bps", state: "succeeded", want: "succeeded"},
		{name: "client_mismatch", profile: "bps", state: "succeeded", wrongClient: true, want: "failed"},
		{name: "not_registered", profile: "codex", state: "succeeded", unsaved: true, want: "failed"},
		{name: "extra_verification", profile: "bps", state: "needs_user_action", want: "needs_user_action"},
		{name: "sanitized_failure", profile: "codex", state: "failed", want: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var imports atomic.Int32
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v8/management/oauth/import" || r.URL.Query().Get("provider") != test.profile || r.Header.Get("Authorization") != "Bearer CPA_SECRET" {
					t.Error("CPA契约或认证错误")
				}
				var result credentials
				_ = json.NewDecoder(r.Body).Decode(&result)
				if result.AccessToken != "ACCESS_SECRET" {
					t.Error("token未到CPA")
				}
				imports.Add(1)
				jsonReply(w, map[string]any{"status": "ok", "provider": test.profile, "auth_id": "channel-a1.json", "saved": true, "registered": !test.unsaved})
			}))
			defer cpa.Close()
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer WORKER_SECRET" {
					t.Error("Worker认证缺失")
				}
				switch r.Method {
				case "POST":
					var in Input
					_ = json.NewDecoder(r.Body).Decode(&in)
					if in.Password != "PASSWORD_SECRET" || in.TOTPSecret != "TOTP_SECRET" {
						t.Error("密码管道前输入不正确")
					}
					jsonReply(w, workerTask{ID: in.TaskID, Status: "running"})
				case "GET":
					c := fakeCredentials(test.profile)
					if test.wrongClient {
						c.ClientID = "wrong"
					}
					jsonReply(w, workerTask{Status: test.state, Result: c, ErrorCode: "upstream PASSWORD_SECRET https://callback?code=SECRET"})
				case "DELETE":
					jsonReply(w, workerTask{Status: "cancelled"})
				}
			}))
			defer worker.Close()
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "CPA_SECRET"}}, nil, worker.URL, "WORKER_SECRET")
			s.poll = time.Millisecond
			task, err := s.Create(Input{Profile: test.profile, Email: "test@example.invalid", Password: "PASSWORD_SECRET", TOTPSecret: "TOTP_SECRET"})
			if err != nil {
				t.Fatal(err)
			}
			task = waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) })
			if task.Status != test.want {
				t.Fatalf("status=%s code=%s", task.Status, task.ErrorCode)
			}
			out, _ := json.Marshal(task)
			for _, secret := range []string{"ACCESS_SECRET", "REFRESH_SECRET", "PASSWORD_SECRET", "TOTP_SECRET", "ID_SECRET", "callback"} {
				if strings.Contains(string(out), secret) {
					t.Fatalf("浏览器结果泄露: %s", secret)
				}
			}
			if test.wrongClient && imports.Load() != 0 {
				t.Fatal("错client仍写入CPA")
			}
		})
	}
}
func TestTaskCancelTimeoutAndBusy(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "timeout"}[expire], func(t *testing.T) {
			var stopped atomic.Bool
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					var in Input
					_ = json.NewDecoder(r.Body).Decode(&in)
					jsonReply(w, workerTask{ID: in.TaskID, Status: "running"})
				} else if r.Method == "DELETE" {
					stopped.Store(true)
					jsonReply(w, workerTask{Status: "cancelled"})
				} else {
					jsonReply(w, workerTask{Status: "running"})
				}
			}))
			defer worker.Close()
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: "http://unused.invalid", ManagementKey: "k"}}, nil, worker.URL, "secret")
			s.poll = time.Millisecond
			if expire {
				s.timeout = 25 * time.Millisecond
			}
			input := Input{Profile: "codex", Email: "fake@example.invalid", Password: "p", TOTPSecret: "s"}
			task, err := s.Create(input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Create(input); !errors.Is(err, ErrBusy) {
				t.Fatal("未限制单任务")
			}
			waitTask(t, s, task.ID, func(v Task) bool { return v.Status == "running" })
			want := "expired"
			if !expire {
				want = "cancelled"
				_, _ = s.Cancel(task.ID)
			}
			got := waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) })
			if got.Status != want || !stopped.Load() {
				t.Fatalf("终止未确认: %+v stopped=%v", got, stopped.Load())
			}
		})
	}
}
func TestImportCannotClaimCancelled(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		jsonReply(w, map[string]any{"status": "ok", "provider": "codex", "auth_id": "a.json", "saved": true, "registered": true})
	}))
	defer cpa.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var in Input
			_ = json.NewDecoder(r.Body).Decode(&in)
			jsonReply(w, workerTask{ID: in.TaskID})
		} else if r.Method == "GET" {
			jsonReply(w, workerTask{Status: "succeeded", Result: fakeCredentials("codex")})
		} else {
			jsonReply(w, workerTask{Status: "succeeded"})
		}
	}))
	defer worker.Close()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "k"}}, nil, worker.URL, "s")
	task, _ := s.Create(Input{Profile: "codex", Email: "e", Password: "p", TOTPSecret: "s"})
	<-entered
	if _, err := s.Cancel(task.ID); !errors.Is(err, ErrImporting) {
		t.Fatal("导入开始后虚报取消")
	}
	close(release)
	if got := waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) }); got.Status != "succeeded" {
		t.Fatalf("导入结果丢失: %+v", got)
	}
}
func TestTargetDriftRejectsTaskAndOAuth(t *testing.T) {
	var writes atomic.Int32
	ready := make(chan struct{})
	once := sync.Once{}
	allow := make(chan struct{})
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "auth-url") {
			jsonReply(w, map[string]string{"url": "https://auth.example.invalid/authorize", "state": "state"})
			return
		}
		writes.Add(1)
		jsonReply(w, map[string]string{"status": "ok"})
	}))
	defer cpa.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var in Input
			_ = json.NewDecoder(r.Body).Decode(&in)
			jsonReply(w, workerTask{ID: in.TaskID})
		} else if r.Method == "GET" {
			once.Do(func() { close(ready) })
			<-allow
			jsonReply(w, workerTask{Status: "succeeded", Result: fakeCredentials("bps")})
		} else {
			jsonReply(w, workerTask{Status: "cancelled"})
		}
	}))
	defer worker.Close()
	resolver := &resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "key"}}
	s := New(resolver, nil, worker.URL, "secret")
	if _, err := s.OAuth(context.Background(), "POST", "auth-url", map[string]string{"provider": "bps"}); err != nil {
		t.Fatal(err)
	}
	task, _ := s.Create(Input{Profile: "bps", Email: "e", Password: "p", TOTPSecret: "s"})
	<-ready
	resolver.setURL("http://different.invalid")
	close(allow)
	task = waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) })
	if task.ErrorCode != "cpa_target_changed" {
		t.Fatalf("目标漂移未拒绝: %+v", task)
	}
	if _, err := s.OAuth(context.Background(), "POST", "callback", map[string]string{"state": "state", "redirect_url": "https://bps.openai.com/callback?state=state&code=fake"}); !errors.Is(err, ErrTargetChanged) {
		t.Fatalf("OAuth目标漂移未拒绝: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatal("目标漂移后仍写入")
	}
	for _, action := range []string{"import", "auth-files", "../config"} {
		if _, err := s.OAuth(context.Background(), "POST", action, map[string]string{}); !errors.Is(err, ErrInput) {
			t.Fatal("额外v8路径被放行")
		}
	}
}

func TestOAuthMutationHeldUntilRegisteredStatus(t *testing.T) {
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "auth-url") {
			jsonReply(w, map[string]string{"url": "https://auth.example.invalid", "state": "state"})
			return
		}
		jsonReply(w, map[string]string{"status": "ok"})
	}))
	defer cpa.Close()
	mutations := cpaauthfiles.NewMutationCoordinator()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "key"}}, mutations, "", "")
	if _, err := s.OAuth(context.Background(), "POST", "auth-url", map[string]string{"provider": "bps"}); err != nil {
		t.Fatal(err)
	}
	callback := map[string]string{"state": "state", "redirect_url": "https://bps.openai.com/callback?state=state&code=fake"}
	if _, err := s.OAuth(context.Background(), "POST", "callback", callback); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if release, err := mutations.AcquireAll(ctx); err == nil {
		release()
		t.Fatal("回调接受后未等待真实保存状态")
	}
	if _, err := s.OAuth(context.Background(), "POST", "callback", callback); !errors.Is(err, ErrBusy) {
		t.Fatal("重复回调被接受")
	}
	if _, err := s.OAuth(context.Background(), "GET", "status", map[string]string{"state": "state"}); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	release, err := mutations.AcquireAll(ctx2)
	if err != nil {
		t.Fatal("保存终态未释放写锁")
	}
	release()
}
