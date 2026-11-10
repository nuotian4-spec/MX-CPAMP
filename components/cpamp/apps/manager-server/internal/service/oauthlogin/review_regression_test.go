package oauthlogin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/cpaauthfiles"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallbackRejectedCanCorrectAndUnknownRetainsLock(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "400_corrected", true: "timeout_retains_lock"}[timeout], func(t *testing.T) {
			var calls atomic.Int32
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "auth-url"):
					jsonReply(w, map[string]string{"url": "https://auth.invalid", "state": "state"})
				case strings.HasSuffix(r.URL.Path, "callback"):
					if calls.Add(1) == 1 {
						if timeout {
							time.Sleep(60 * time.Millisecond)
							jsonReply(w, map[string]string{"status": "ok"})
						} else {
							w.WriteHeader(400)
							jsonReply(w, map[string]string{"error": "invalid_callback_url"})
						}
						return
					}
					jsonReply(w, map[string]string{"status": "ok"})
				default:
					jsonReply(w, map[string]string{"status": "ok"})
				}
			}))
			defer cpa.Close()
			mutations := cpaauthfiles.NewMutationCoordinator()
			s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "k"}}, mutations, "", "")
			s.client.Timeout = 20 * time.Millisecond
			_, err := s.OAuth(context.Background(), "POST", "auth-url", map[string]string{"provider": "bps"})
			if err != nil {
				t.Fatal(err)
			}
			callback := map[string]string{"state": "state", "redirect_url": "https://bps.openai.com/callback?state=state&code=fake"}
			if _, err = s.OAuth(context.Background(), "POST", "callback", callback); err == nil {
				t.Fatal("预期上游拒绝/超时")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			release, lockErr := mutations.AcquireAll(ctx)
			if timeout {
				if lockErr == nil {
					release()
					t.Fatal("保存未知时放开写锁")
				}
				if _, err = s.OAuth(context.Background(), "POST", "callback", callback); !errors.Is(err, ErrBusy) {
					t.Fatal("超时后盲重提")
				}
			} else {
				if lockErr != nil {
					t.Fatal("明确拒绝仍锁住")
				}
				release()
				if _, err = s.OAuth(context.Background(), "POST", "callback", callback); err != nil {
					t.Fatalf("纠正后重提失败: %v", err)
				}
			}
			_, _ = s.OAuth(context.Background(), "GET", "status", map[string]string{"state": "state"})
		})
	}
}
func TestKnownIDLostResponseAndEarlyCancelNeverImport(t *testing.T) {
	var imports, creates atomic.Int32
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imports.Add(1)
		jsonReply(w, map[string]string{"status": "ok"})
	}))
	defer cpa.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var in Input
			_ = json.NewDecoder(r.Body).Decode(&in)
			creates.Add(1)
			jsonReply(w, workerTask{ID: in.TaskID, Status: "running"})
		} else if r.Method == "GET" {
			jsonReply(w, workerTask{Status: "running"})
		} else {
			jsonReply(w, workerTask{Status: "cancelled"})
		}
	}))
	defer worker.Close()
	s := New(&resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "k"}}, nil, worker.URL, "secret")
	s.poll = time.Millisecond
	input := Input{TaskID: "11111111-1111-4111-8111-111111111111", Profile: "bps", Email: "fake@example.invalid", Password: "p", TOTPSecret: "s"}
	accepted, err := s.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	// 浏览器未拿到创建响应，仍以提交前生成的相同编号查询、取消。
	waitTask(t, s, input.TaskID, func(v Task) bool { return v.Status == "running" })
	same, err := s.Create(input)
	if err != nil || same.ID != accepted.ID {
		t.Fatal("幂等创建丢失")
	}
	_, _ = s.Cancel(input.TaskID)
	if done := waitTask(t, s, input.TaskID, func(v Task) bool { return terminal(v.Status) }); done.Status != "cancelled" {
		t.Fatal(done.Status)
	}
	input.TaskID = "22222222-2222-4222-8222-222222222222"
	_, _ = s.Cancel(input.TaskID)
	early, err := s.Create(input)
	if err != nil || early.Status != "cancelled" {
		t.Fatal("离页先到的取消未阻止迟到POST")
	}
	if imports.Load() != 0 || creates.Load() != 1 {
		t.Fatalf("imports=%d creates=%d", imports.Load(), creates.Load())
	}
}
