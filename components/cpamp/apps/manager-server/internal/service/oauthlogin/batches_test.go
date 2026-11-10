package oauthlogin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type batchFixture struct {
	mu            sync.Mutex
	posts         []Input
	deleted       map[string]bool
	clears        int
	block         bool
	cleanupFail   bool
	importUnknown bool
	importStarted chan struct{}
	allowImport   chan struct{}
	resolver      *resolver
	service       *Service
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	f := &batchFixture{deleted: map[string]bool{}}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.importStarted != nil {
			close(f.importStarted)
			<-f.allowImport
		}
		if f.importUnknown {
			w.WriteHeader(502)
			return
		}
		jsonReply(w, map[string]any{"status": "ok", "provider": "codex", "auth_id": "account.json", "saved": true, "registered": true})
	}))
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/tasks/")
		switch r.Method {
		case "POST":
			var input Input
			_ = json.NewDecoder(r.Body).Decode(&input)
			if len(f.posts) > 0 && !f.deleted[f.posts[len(f.posts)-1].TaskID] {
				t.Error("上一条未清理就启动下一条")
			}
			f.posts = append(f.posts, input)
			jsonReply(w, workerTask{ID: input.TaskID, Status: "running"})
		case "GET":
			if f.block {
				jsonReply(w, workerTask{ID: id, Status: "running"})
				return
			}
			if len(f.posts) > 0 && f.posts[len(f.posts)-1].Email == "fail@example.invalid" {
				jsonReply(w, workerTask{ID: id, Status: "needs_user_action", Stage: "password", Reason: "security_check"})
				return
			}
			jsonReply(w, workerTask{ID: id, Status: "succeeded", Result: fakeCredentials("codex")})
		case "DELETE":
			if r.URL.Path == "/tasks" {
				jsonReply(w, map[string]bool{"ok": true})
				return
			}
			if f.cleanupFail {
				w.WriteHeader(503)
				return
			}
			f.deleted[id] = true
			f.clears++
			jsonReply(w, map[string]any{"status": "cancelled", "result_cleared": true})
		}
	}))
	f.resolver = &resolver{setup: store.Setup{CPAUpstreamURL: cpa.URL, ManagementKey: "secret"}}
	f.service = New(f.resolver, nil, worker.URL, "worker")
	f.service.poll = time.Millisecond
	t.Cleanup(worker.Close)
	t.Cleanup(cpa.Close)
	return f
}
func batchInput(emails ...string) BatchInput {
	in := BatchInput{BatchID: "11111111-1111-4111-8111-111111111111", Profile: "codex"}
	for i, email := range emails {
		in.Items = append(in.Items, BatchInputItem{LineNo: i + 1, Email: email, Password: "PASSWORD_CANARY", TOTPSecret: "ABCDEFGHIJKLMNOP"})
	}
	return in
}
func waitBatch(t *testing.T, s *Service, id string, predicate func(Batch) bool) Batch {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := s.GetBatch(id)
		if err == nil && predicate(b) {
			return b
		}
		time.Sleep(time.Millisecond)
	}
	b, _ := s.GetBatch(id)
	t.Fatalf("批次未收敛: %+v", b)
	return b
}
func TestBatchSerialValidationAndPrivacy(t *testing.T) {
	f := newBatchFixture(t)
	in := batchInput("one@example.invalid", "fail@example.invalid", "one@example.invalid", "bad", "last@example.invalid")
	b, err := f.service.CreateBatch(in)
	if err != nil {
		t.Fatal(err)
	}
	b = waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
	if b.Status != "completed" {
		t.Fatalf("%+v", b)
	}
	wants := []string{"succeeded", "needs_user_action", "skipped", "skipped", "succeeded"}
	for i, want := range wants {
		if b.Items[i].Status != want {
			t.Fatalf("row%d=%+v", i, b.Items[i])
		}
	}
	raw, _ := json.Marshal(b)
	for _, secret := range []string{"PASSWORD_CANARY", "ABCDEFGHIJKLMNOP", "ACCESS_SECRET", "one@example.invalid"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("摘要泄露%s", secret)
		}
	}
	if b.Items[1].Stage != "password" || b.Items[1].Reason != "security_check" {
		t.Fatal("丢失逐项诊断")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 3 {
		t.Fatalf("执行%d次", len(f.posts))
	}
}
func TestBatchIdempotencyAndExclusiveChannel(t *testing.T) {
	f := newBatchFixture(t)
	f.block = true
	in := batchInput("one@example.invalid", "two@example.invalid")
	b, err := f.service.CreateBatch(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateBatch(batchInput("one@example.invalid")); err != nil {
		t.Fatal("相同编号未去重", err)
	}
	if _, err = f.service.Create(Input{Profile: "codex", Email: "other@example.invalid", Password: "p", TOTPSecret: "s"}); err != ErrBusy {
		t.Fatal("单任务插队", err)
	}
	_, _ = f.service.CancelBatch(b.ID)
	b = waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
	if b.Status != "cancelled" {
		t.Fatalf("%+v", b)
	}
	f.service.mu.Lock()
	defer f.service.mu.Unlock()
	for _, input := range f.service.batches[b.ID].pending {
		if input != nil {
			t.Fatal("取消后仍持有未开始材料")
		}
	}
}
func TestBatchWaitingTTLWithoutPolling(t *testing.T) {
	f := newBatchFixture(t)
	f.block = true
	f.service.batchTTL = 20 * time.Millisecond
	b, err := f.service.CreateBatch(batchInput("one@example.invalid", "two@example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	waitBatch(t, f.service, b.ID, func(b Batch) bool { return b.Items[0].Status == "running" })
	time.Sleep(35 * time.Millisecond)
	f.service.mu.Lock()
	state := f.service.batches[b.ID]
	if state.pending[1] != nil || state.Items[1].Status != "expired" {
		t.Error("后台TTL未清理未开始材料")
	}
	f.service.mu.Unlock()
	_, _ = f.service.CancelBatch(b.ID)
	waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
}
func TestBatchInterruptsUnknownResultOrCleanup(t *testing.T) {
	for _, kind := range []string{"import", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			f := newBatchFixture(t)
			f.importUnknown = kind == "import"
			f.cleanupFail = kind == "cleanup"
			b, err := f.service.CreateBatch(batchInput("one@example.invalid", "two@example.invalid"))
			if err != nil {
				t.Fatal(err)
			}
			b = waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
			if b.Status != "interrupted" || b.Items[1].Status != "cancelled" {
				t.Fatalf("%+v", b)
			}
			if kind == "cleanup" && b.Items[0].Status != "succeeded" {
				t.Fatal("清理失败不得抹去已保存结果")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.posts) != 1 {
				t.Fatal("未知结果后继续登录")
			}
		})
	}
}
func TestBatchStopsWhenTargetChanges(t *testing.T) {
	f := newBatchFixture(t)
	f.block = true
	b, err := f.service.CreateBatch(batchInput("one@example.invalid", "two@example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	waitBatch(t, f.service, b.ID, func(b Batch) bool { return b.Items[0].Status == "running" })
	f.resolver.setURL("http://other.invalid")
	f.mu.Lock()
	f.block = false
	f.mu.Unlock()
	b = waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
	if b.Status != "interrupted" || b.ErrorCode != "cpa_target_changed" {
		t.Fatalf("%+v", b)
	}
}
func TestCleanupRequiresExplicitConfirmation(t *testing.T) {
	for _, status := range []int{200, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if status == 200 {
					jsonReply(w, map[string]string{"status": "cancelled"})
				}
			}))
			defer worker.Close()
			s := New(nil, nil, worker.URL, "s")
			if s.cleanupWorker("task") != (status == 404) {
				t.Fatal("清理确认含义不正确")
			}
		})
	}
}

func TestBatchCancellationWaitsForImportResult(t *testing.T) {
	f := newBatchFixture(t)
	f.importStarted = make(chan struct{})
	f.allowImport = make(chan struct{})
	b, err := f.service.CreateBatch(batchInput("one@example.invalid", "two@example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.importStarted
	cancelled, err := f.service.CancelBatch(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelling" || cancelled.Items[0].Status != "importing" || cancelled.Items[1].Status != "cancelled" {
		t.Fatalf("%+v", cancelled)
	}
	close(f.allowImport)
	b = waitBatch(t, f.service, b.ID, func(b Batch) bool { return !batchActive(b.Status) })
	if b.Status != "cancelled" || b.Items[0].Status != "succeeded" || b.Items[0].AuthID == "" {
		t.Fatalf("导入结果被取消覆盖: %+v", b)
	}
}
