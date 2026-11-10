package oauthlogin

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type BatchInput struct {
	BatchID string           `json:"batch_id"`
	Profile string           `json:"profile"`
	Items   []BatchInputItem `json:"items"`
}
type BatchInputItem struct {
	LineNo     int    `json:"line_no"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
}
type BatchItem struct {
	LineNo    int    `json:"line_no"`
	EmailHint string `json:"email_hint"`
	Task
}
type Batch struct {
	ID        string      `json:"id"`
	Profile   string      `json:"profile"`
	Status    string      `json:"status"`
	ErrorCode string      `json:"error_code,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	Items     []BatchItem `json:"items"`
}
type batchState struct {
	Batch
	setup   store.Setup
	pending []*Input
	current string
	timer   *time.Timer
}

var batchTOTP = regexp.MustCompile(`^[A-Z2-7=]{16,160}$`)

func taskDone(t *Task) bool {
	if t.done == nil {
		return true
	}
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}
func batchActive(status string) bool { return status == "running" || status == "cancelling" }
func batchSnapshot(b *batchState) Batch {
	out := b.Batch
	out.Items = append([]BatchItem(nil), b.Items...)
	for i := range out.Items {
		out.Items[i].Task = snapshot(&out.Items[i].Task)
	}
	return out
}
func emailHint(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || len(parts[0]) == 0 {
		return "无效邮箱"
	}
	return string([]rune(parts[0])[:1]) + "***@" + parts[1]
}
func clearInput(input *Input) {
	if input != nil {
		input.Password = ""
		input.TOTPSecret = ""
		input.Email = ""
	}
}
func (s *Service) CreateBatch(input BatchInput) (Batch, error) {
	defer func() {
		for i := range input.Items {
			input.Items[i] = BatchInputItem{}
		}
	}()
	if !validTaskID(input.BatchID) || (input.Profile != "codex" && input.Profile != "bps") || len(input.Items) == 0 || len(input.Items) > 100 {
		return Batch{}, ErrInput
	}
	setup, ok, err := s.resolver.ResolveSetup(context.Background())
	if err != nil || !ok || setup.CPAUpstreamURL == "" || setup.ManagementKey == "" {
		return Batch{}, ErrUnavailable
	}
	s.mu.Lock()
	if existing := s.batches[input.BatchID]; existing != nil {
		defer s.mu.Unlock()
		if existing.Profile != input.Profile || !sameSetup(existing.setup, setup) {
			return Batch{}, ErrInput
		}
		return batchSnapshot(existing), nil
	}
	if s.activeBatch != "" {
		s.mu.Unlock()
		return Batch{}, ErrBusy
	}
	for _, t := range s.tasks {
		if !terminal(t.Status) || !taskDone(t) {
			s.mu.Unlock()
			return Batch{}, ErrBusy
		}
	}
	if s.workerURL == "" || s.secret == "" {
		s.mu.Unlock()
		return Batch{}, ErrUnavailable
	}
	now := time.Now().UTC()
	b := &batchState{Batch: Batch{ID: input.BatchID, Profile: input.Profile, Status: "running", CreatedAt: now, ExpiresAt: now.Add(s.batchTTL), Items: make([]BatchItem, len(input.Items))}, setup: setup, pending: make([]*Input, len(input.Items))}
	seen := map[string]bool{}
	lines := map[int]bool{}
	for i, item := range input.Items {
		email := strings.ToLower(strings.TrimSpace(item.Email))
		secret := strings.ToUpper(strings.Join(strings.Fields(item.TOTPSecret), ""))
		row := BatchItem{LineNo: item.LineNo, EmailHint: emailHint(email), Task: Task{Profile: input.Profile, Status: "pending", CreatedAt: now}}
		address, parseErr := mail.ParseAddress(email)
		if item.LineNo <= 0 || lines[item.LineNo] || len(item.Email)+len(item.Password)+len(item.TOTPSecret) > 16*1024 || parseErr != nil || address.Address != email || item.Password == "" || len(item.Password) > 4096 || !batchTOTP.MatchString(secret) {
			row.Status, row.ErrorCode = "skipped", "invalid_row"
		} else if seen[email] {
			row.Status, row.ErrorCode = "skipped", "duplicate_email"
		} else {
			seen[email] = true
			b.pending[i] = &Input{Profile: input.Profile, Email: email, Password: item.Password, TOTPSecret: secret}
		}
		lines[item.LineNo] = true
		b.Items[i] = row
	}
	s.batches[b.ID] = b
	s.activeBatch = b.ID
	b.timer = time.AfterFunc(s.batchTTL, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.batches[b.ID] != b {
			return
		}
		s.clearPendingLocked(b, "expired", "queue_expired")
	})
	out := batchSnapshot(b)
	s.mu.Unlock()
	go s.runBatch(b.ID)
	return out, nil
}
func (s *Service) GetBatch(id string) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[id]
	if b == nil {
		return Batch{}, ErrNotFound
	}
	return s.batchViewLocked(b), nil
}
func (s *Service) ActiveBatch() (*Batch, error) {
	setup, ok, err := s.resolver.ResolveSetup(context.Background())
	if err != nil || !ok {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[s.activeBatch]
	if b == nil || !sameSetup(b.setup, setup) {
		return nil, nil
	}
	out := s.batchViewLocked(b)
	return &out, nil
}
func (s *Service) batchViewLocked(b *batchState) Batch {
	out := batchSnapshot(b)
	for i := range out.Items {
		if t := s.tasks[out.Items[i].ID]; t != nil {
			out.Items[i].Task = snapshot(t)
		}
	}
	return out
}
func (s *Service) clearPendingLocked(b *batchState, status, code string) {
	for i, input := range b.pending {
		if input == nil {
			continue
		}
		clearInput(input)
		b.pending[i] = nil
		b.Items[i].Status, b.Items[i].ErrorCode = status, code
	}
}
func (s *Service) CancelBatch(id string) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[id]
	if b == nil {
		return Batch{}, ErrNotFound
	}
	if !batchActive(b.Status) {
		return s.batchViewLocked(b), nil
	}
	b.Status = "cancelling"
	s.clearPendingLocked(b, "cancelled", "")
	if t := s.tasks[b.current]; t != nil && !terminal(t.Status) && t.Status != "importing" {
		t.Status = "cancelling"
		t.cancel()
	}
	return s.batchViewLocked(b), nil
}
func (s *Service) endBatchLocked(b *batchState, status, code string) {
	b.Status, b.ErrorCode = status, code
	s.clearPendingLocked(b, "cancelled", code)
	b.timer.Stop()
	if s.activeBatch == b.ID {
		s.activeBatch = ""
	}
	time.AfterFunc(s.batchRetention, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.batches[b.ID] == b {
			delete(s.batches, b.ID)
		}
	})
}
func (s *Service) runBatch(id string) {
	for i := 0; ; i++ {
		s.mu.Lock()
		b := s.batches[id]
		if b.Status == "cancelling" {
			s.endBatchLocked(b, "cancelled", "")
			s.mu.Unlock()
			return
		}
		if i >= len(b.pending) {
			s.endBatchLocked(b, "completed", "")
			s.mu.Unlock()
			return
		}
		if time.Now().After(b.ExpiresAt) {
			s.clearPendingLocked(b, "expired", "queue_expired")
		}
		input := b.pending[i]
		if input == nil {
			s.mu.Unlock()
			continue
		}
		// 当前条目立即交给单任务；其余材料仍由批次定时清除。
		b.pending[i] = nil
		expected := b.setup
		s.mu.Unlock()
		task, err := s.createBound(*input, &expected, id)
		clearInput(input)
		s.mu.Lock()
		if err != nil {
			if b.Status == "cancelling" {
				b.Items[i].Status = "cancelled"
				s.endBatchLocked(b, "cancelled", "")
				s.mu.Unlock()
				return
			}
			code := "worker_unavailable"
			if errors.Is(err, ErrTargetChanged) {
				code = "cpa_target_changed"
			}
			b.Items[i].Status, b.Items[i].ErrorCode = "failed", code
			s.endBatchLocked(b, "interrupted", code)
			s.mu.Unlock()
			return
		}
		b.current = task.ID
		b.Items[i].Task = task
		live := s.tasks[task.ID]
		// 取消可能在创建过程中到达；导入阶段仍等待真实结果。
		if b.Status == "cancelling" && !terminal(live.Status) && live.Status != "importing" {
			live.Status = "cancelling"
			live.cancel()
		}
		done := live.done
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		b.Items[i].Task = snapshot(live)
		b.current = ""
		code := live.ErrorCode
		if !live.cleanupConfirmed {
			code = "worker_cleanup_unconfirmed"
		}
		switch code {
		case "worker_cleanup_unconfirmed", "worker_stop_unconfirmed", "worker_unavailable", "import_unconfirmed", "cpa_target_changed", "import_busy":
			s.endBatchLocked(b, "interrupted", code)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

type workerCleanupResponse struct {
	ResultCleared bool   `json:"result_cleared"`
	Status        string `json:"status"`
}

func (s *Service) cleanupWorker(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		var result workerCleanupResponse
		if err := s.worker(ctx, http.MethodDelete, "/tasks/"+url.PathEscape(id), nil, &result); err != nil {
			return false
		}
		if result.ResultCleared {
			return true
		}
		if !terminal(result.Status) {
			return false
		}
	}
	return false
}
