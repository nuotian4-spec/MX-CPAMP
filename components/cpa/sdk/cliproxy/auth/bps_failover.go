package auth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MoeForge/moego/ojson"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type bpsRequestKey struct{}
type bpsDeliveryKey struct{}

// WithBPSClientDelivery shares pre-bootstrap writes without inspecting a concurrent HTTP writer.
func WithBPSClientDelivery(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if delivered, ok := ctx.Value(bpsDeliveryKey{}).(*atomic.Bool); ok {
		return ctx, func() { delivered.Store(true) }
	}
	delivered := &atomic.Bool{}
	return context.WithValue(ctx, bpsDeliveryKey{}, delivered), func() { delivered.Store(true) }
}

func bpsClientDelivered(ctx context.Context) bool {
	delivered, _ := ctx.Value(bpsDeliveryKey{}).(*atomic.Bool)
	return delivered != nil && delivered.Load()
}

// bpsRequestState belongs to the client request, not a scheduler retry round.
type bpsRequestState struct {
	wire []byte
	sync.Mutex
	independent, delivered, pending bool
	attempts                        int
	upstreamResponses               int
	corrections                     int
	authID, account, subject, model string
	payload                         []byte
	failure                         error
}

// BeginBPSToolCorrection permits two corrections and excludes account failover.
func BeginBPSToolCorrection(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := bpsState(ctx)
	if s == nil {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	if s.attempts > 1 || s.pending || s.corrections >= 2 {
		return NewRequestScopedError("BPS工具纠正预算已用尽", 502)
	}
	s.corrections++
	s.independent = false
	return nil
}

func WithBPSRequestState(ctx context.Context, providers []string, req coreexecutor.Request, opts coreexecutor.Options) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if bpsState(ctx) != nil {
		return ctx
	}
	for _, provider := range providers {
		if strings.EqualFold(provider, "bps") {
			independent := len(providers) == 1 && bpsIndependentRequest(req.Payload) && pinnedAuthIDFromMetadata(opts.Metadata) == ""
			if len(opts.OriginalRequest) != 0 {
				independent = independent && bpsIndependentRequest(opts.OriginalRequest)
			}
			if session, _ := opts.Metadata[coreexecutor.ExecutionSessionMetadataKey].(string); session != "" {
				independent = false
			}
			return context.WithValue(ctx, bpsRequestKey{}, &bpsRequestState{independent: independent})
		}
	}
	return ctx
}

func bpsState(ctx context.Context) *bpsRequestState {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(bpsRequestKey{}).(*bpsRequestState)
	return s
}

// BPSAutomaticRetryBlocked prevents outer bootstrap from creating another budget.
func BPSAutomaticRetryBlocked(ctx context.Context) bool {
	s := bpsState(ctx)
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	return s.attempts > 0
}

// MarkBPSClientEvent is called at the handler's actual delivery boundary.
func MarkBPSClientEvent(ctx context.Context) {
	if delivered, ok := ctx.Value(bpsDeliveryKey{}).(*atomic.Bool); ok {
		delivered.Store(true)
	}
	if s := bpsState(ctx); s != nil {
		s.Lock()
		s.delivered = true
		s.Unlock()
	}
}

func bpsIndependentRequest(raw []byte) bool {
	if !ojson.ValidBytes(raw) || coreexecutor.HasAccountBoundHistory(raw) {
		return false
	}
	r := ojson.ParseBytes(raw)
	if !r.IsObject() || len(r.Get("tools").Array()) == 0 {
		return false
	}
	return bpsTextInput(raw)
}

func bpsTextInput(raw []byte) bool {
	r := ojson.ParseBytes(raw)
	if coreexecutor.HasAccountBoundHistory(raw) {
		return false
	}
	for _, key := range []string{"previous_response_id", "conversation", "attachments", "files", "encrypted_content"} {
		if r.Get(key).Exists() {
			return false
		}
	}
	input := r.Get("input")
	if !input.Exists() {
		input = r.Get("messages")
	}
	if input.Type == ojson.String {
		return true
	}
	if !input.IsArray() || len(input.Array()) == 0 {
		return false
	}
	for _, msg := range input.Array() {
		if !msg.IsObject() {
			return false
		}
		if kind := msg.Get("type").String(); kind != "" && kind != "message" {
			return false
		}
		switch msg.Get("role").String() {
		case "user", "assistant", "system", "developer":
		default:
			return false
		}
		for _, key := range []string{"id", "file_id", "attachment_id", "encrypted_content", "tool_calls", "function_call"} {
			if msg.Get(key).Exists() {
				return false
			}
		}
		content := msg.Get("content")
		if content.Type == ojson.String {
			continue
		}
		if !content.IsArray() {
			return false
		}
		for _, part := range content.Array() {
			switch part.Get("type").String() {
			case "text", "input_text", "output_text":
			default:
				return false
			}
			if part.Get("text").Type != ojson.String {
				return false
			}
		}
	}
	return true
}

func beginBPSDispatch(ctx context.Context, auth *Auth, req coreexecutor.Request, opts coreexecutor.Options) error {
	s := bpsState(ctx)
	if s == nil || auth == nil {
		return nil
	}
	s.Lock()
	defer s.Unlock()
	if s.attempts == 0 {
		if auth.Provider != "bps" {
			return nil
		}
		s.independent = s.independent && bpsIndependentRequest(req.Payload) && pinnedAuthIDFromMetadata(opts.Metadata) == ""
		s.attempts = 1
		s.authID, s.account, s.subject, s.model = auth.ID, authMetadataString(auth, "account_id"), authMetadataString(auth, "sub"), req.Model
		s.payload = bytes.Clone(req.Payload)
		return nil
	}
	if pinnedAuthIDFromMetadata(opts.Metadata) == "" && auth.Provider == "bps" && s.pending && s.attempts == 1 && !s.delivered && !bpsClientDelivered(ctx) && ctx.Err() == nil && auth.ID != s.authID && s.account != "" && s.subject != "" && authMetadataString(auth, "account_id") != "" && authMetadataString(auth, "sub") != "" && (authMetadataString(auth, "account_id") != s.account || authMetadataString(auth, "sub") != s.subject) && req.Model == s.model && bytes.Equal(req.Payload, s.payload) {
		s.pending = false
		s.attempts++
		return nil
	}
	if s.failure != nil {
		return wrapRequestStopError(s.failure)
	}
	return wrapRequestStopError(NewRequestScopedError("BPS request generation budget exhausted", 502))
}

// CheckBPSFailoverPayload compares finalized business payloads before the second POST.
func CheckBPSFailoverPayload(ctx context.Context, body []byte) error {
	s := bpsState(ctx)
	if s == nil {
		return nil
	}
	doc := ojson.NewDoc(body)
	for _, key := range []string{"metadata.task_id", "metadata.turn_id", "prompt_cache_key"} {
		_ = doc.Delete(key)
	}
	value := doc.Bytes()
	s.Lock()
	defer s.Unlock()
	if s.wire == nil {
		s.independent = s.independent && bpsTextInput(body)
		s.wire = value
		return nil
	}
	if s.attempts > 1 && (s.delivered || bpsClientDelivered(ctx) || ctx.Err() != nil) {
		return NewRequestScopedError("BPS failover stopped before dispatch", 409)
	}
	if !bytes.Equal(s.wire, value) {
		return NewRequestScopedError("BPS failover final payload changed", 409)
	}
	return nil
}

func BPSGenerationCount(ctx context.Context) int {
	s := bpsState(ctx)
	if s == nil {
		return 0
	}
	s.Lock()
	defer s.Unlock()
	return s.attempts
}

// RecordBPSUpstreamResponse counts actual HTTP receipts, not selection or admission.
func RecordBPSUpstreamResponse(ctx context.Context) {
	if s := bpsState(ctx); s != nil {
		s.Lock()
		defer s.Unlock()
		if s.upstreamResponses < s.attempts && s.upstreamResponses < 2 {
			s.upstreamResponses++
		}
	}
}

// BPSFailoverResponseHeaders reports regeneration without promising stream completion.
// Apply after upstream filtering and interception; upstream claims are not trusted.
func BPSFailoverResponseHeaders(ctx context.Context, headers http.Header) http.Header {
	s := bpsState(ctx)
	if s == nil {
		return headers
	}
	const header = "X-BPS-Failover-Count"
	for name := range headers {
		if strings.EqualFold(name, header) {
			delete(headers, name)
		}
	}
	s.Lock()
	defer s.Unlock()
	if s.upstreamResponses == 2 {
		if headers == nil {
			headers = make(http.Header)
		}
		headers.Set(header, "1")
	}
	return headers
}

func (m *Manager) excludeBPSFailoverCandidates(ctx context.Context, tried map[string]struct{}, model string) {
	if !bpsFailoverPending(ctx) {
		return
	}
	for _, a := range m.List() {
		models, _, _, _ := m.executionModelCandidatesWithAlias(a, model)
		if len(bpsFailoverModels(ctx, a, models)) == 0 {
			tried[a.ID] = struct{}{}
		}
	}
}

// authorizeBPSFailover requires typed local facts, never an HTTP code or message.
func authorizeBPSFailover(ctx context.Context, err error) bool {
	s := bpsState(ctx)
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	s.failure = err
	var facts interface{ BPSFailoverFacts() (bool, bool) }
	completed, withheld := false, false
	if errors.As(err, &facts) {
		completed, withheld = facts.BPSFailoverFacts()
	}
	var classification interface{ BPSFailoverReason() (string, string) }
	if errors.As(err, &classification) {
		reason, validation := classification.BPSFailoverReason()
		if (reason == "schema_validation" && validation != "schema_mismatch") || (validation != "" && validation != "schema_mismatch") {
			return false
		}
	}
	if ctx.Err() != nil || !s.independent || s.delivered || bpsClientDelivered(ctx) || s.pending || s.attempts != 1 || s.account == "" || s.subject == "" || !completed || !withheld {
		return false
	}
	s.pending = true
	return true
}

func bpsFailoverPending(ctx context.Context) bool {
	s := bpsState(ctx)
	if s == nil {
		return false
	}
	s.Lock()
	defer s.Unlock()
	return s.pending
}

// Filter only the existing authorized pool; aliases must resolve to A's exact model.
func bpsFailoverModels(ctx context.Context, auth *Auth, models []string) []string {
	s := bpsState(ctx)
	if s == nil {
		return models
	}
	s.Lock()
	defer s.Unlock()
	if s.attempts == 0 {
		return models
	}
	if !s.pending || auth.Provider != "bps" || auth.ID == s.authID || authMetadataString(auth, "account_id") == "" || authMetadataString(auth, "sub") == "" || authMetadataString(auth, "account_id") == s.account && authMetadataString(auth, "sub") == s.subject {
		return nil
	}
	for _, model := range models {
		if model == s.model {
			return []string{model}
		}
	}
	return nil
}
