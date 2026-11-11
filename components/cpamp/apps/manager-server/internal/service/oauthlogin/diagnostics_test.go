package oauthlogin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func TestDiagnosticWhitelistAndTerminalPropagation(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				var input Input
				_ = json.NewDecoder(r.Body).Decode(&input)
				jsonReply(w, workerTask{ID: input.TaskID, Status: "running"})
			case http.MethodGet:
				stage, reason := "password", "security_check"
				if invalid {
					stage, reason = "PASSWORD_SECRET", "COOKIE_SECRET"
				}
				jsonReply(w, workerTask{Status: "needs_user_action", ErrorCode: "needs_user_action", Stage: stage, Reason: reason})
			case http.MethodDelete:
				jsonReply(w, workerTask{Status: "cancelled"})
			}
		}))
		s := New(&resolver{setup: store.Setup{CPAUpstreamURL: "http://unused.invalid", ManagementKey: "key"}}, nil, worker.URL, "secret")
		s.poll = time.Millisecond
		task, err := s.Create(Input{Profile: "codex", Email: "test@example.invalid", Password: "p", TOTPSecret: "s"})
		if err != nil {
			t.Fatal(err)
		}
		done := waitTask(t, s, task.ID, func(v Task) bool { return terminal(v.Status) })
		if invalid {
			if done.Stage != "unknown" || done.Reason != "unknown" {
				t.Fatal("非枚举诊断未被清除")
			}
		} else if done.Stage != "password" || done.Reason != "security_check" {
			t.Fatal("诊断未透传")
		}
		raw, _ := json.Marshal(done)
		if strings.Contains(string(raw), "SECRET") {
			t.Fatal("诊断包含秘密")
		}
		worker.Close()
	}
}

func TestSpecificWorkerFailuresRemainDistinct(t *testing.T) {
	for _, code := range []string{"transport_error", "token_exchange_failed", "identity_mismatch", "password_rejected", "totp_rejected"} {
		if safeCode(code) != code {
			t.Fatalf("错误码丢失: %s", code)
		}
	}
}
