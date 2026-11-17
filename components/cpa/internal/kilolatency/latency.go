// Package kilolatency holds request-local observations without affecting execution.
package kilolatency

import (
	"context"
	"errors"
	"sync"
	"time"
)

type contextKey struct{}
type usageKey struct{}
type sessionWaitKey struct{}

func WithSessionWait(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionWaitKey{}, true)
}
func IsSessionWait(ctx context.Context) bool { v, _ := ctx.Value(sessionWaitKey{}).(bool); return v }

// State is shared by the handler, admission, executor, and binding store.
type State struct {
	mu                                            sync.Mutex
	now                                           func() time.Time
	entry, admitted, sent, headers, text, flushed time.Time
	enabled, stream, tools, ended                 bool
	err                                           error
	totals                                        map[string]time.Duration
	counts                                        map[string]int
}

func NewContext(ctx context.Context) (context.Context, *State) {
	if s := From(ctx); s != nil {
		return ctx, s
	}
	s := &State{now: time.Now, entry: time.Now(), totals: make(map[string]time.Duration), counts: make(map[string]int)}
	return context.WithValue(ctx, contextKey{}, s), s
}

func From(ctx context.Context) *State {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(*State)
	return s
}

func Inherit(ctx, source context.Context) context.Context {
	if s := From(source); s != nil {
		return context.WithValue(ctx, contextKey{}, s)
	}
	return ctx
}

func (s *State) Enable(stream bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled, s.stream = true, stream
}

func (s *State) Enabled() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

func (s *State) Mark(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	var point *time.Time
	switch name {
	case "admitted":
		point = &s.admitted
	case "sent":
		point = &s.sent
	case "headers":
		point = &s.headers
	case "text":
		point = &s.text
	case "flushed":
		point = &s.flushed
	default:
		return
	}
	if point.IsZero() {
		*point = s.now()
	}
}

// Measure records only executed intervals; an absent hook is not a zero sample.
func (s *State) Measure(name string) func() {
	if !s.Enabled() {
		return func() {}
	}
	start := s.now()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.ended {
			s.totals[name] += s.now().Sub(start)
			s.counts[name+"_count"]++
		}
	}
}

func (s *State) Count(name string, value int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled && !s.ended {
		s.counts[name] += value
	}
}

func (s *State) QueueLoad(waiting, active int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.counts["queue_waiting_at_entry"]; !ok {
		s.counts["queue_waiting_at_entry"], s.counts["queue_active_at_entry"] = waiting, active
	}
}

func (s *State) Tool() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = true
}

func (s *State) NeedsQueueLoad() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, sampled := s.counts["queue_waiting_at_entry"]
	return s.enabled && !s.ended && !sampled
}

func (s *State) Failure(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended && s.err == nil {
		s.err = err
	}
}

func (s *State) TTFT() (time.Duration, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent.IsZero() || s.text.IsZero() {
		return 0, false
	}
	return s.text.Sub(s.sent), true
}

// Finish snapshots once, before any normal execution-context cleanup cancellation.
func (s *State) Finish(requestErr error, status int) map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.ended {
		return nil
	}
	s.ended = true
	err := s.err
	if err == nil {
		err = requestErr
	}
	errorStatus := 0
	var statusError interface{ StatusCode() int }
	if errors.As(err, &statusError) {
		errorStatus = statusError.StatusCode()
	}
	outcome := "success_empty"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		outcome = "canceled"
	case errorStatus == 429 || status == 429:
		outcome = "rejected"
	case err != nil || status >= 400:
		outcome = "failed"
	case !s.text.IsZero():
		outcome = "success_text"
	case s.tools:
		outcome = "success_tool"
	}
	mode := "nonstream"
	if s.stream {
		mode = "stream"
	}
	fields := map[string]any{"outcome": outcome, "mode": mode, "status": status, "server_total_ms": milliseconds(s.now().Sub(s.entry))}
	add := func(name string, start, end time.Time) {
		if !start.IsZero() && !end.IsZero() {
			fields[name] = milliseconds(end.Sub(start))
		}
	}
	add("admission_elapsed_ms", s.entry, s.admitted)
	add("upstream_headers_ms", s.sent, s.headers)
	add("upstream_first_text_ms", s.sent, s.text)
	add("cpa_first_text_ms", s.entry, s.flushed)
	add("first_text_forward_ms", s.text, s.flushed)
	for name, duration := range s.totals {
		fields[name+"_ms"] = milliseconds(duration)
	}
	for name, count := range s.counts {
		fields[name] = count
	}
	return fields
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

type usageTTFT struct {
	duration time.Duration
	observed bool
}

// WithUsageTTFT freezes availability at the existing usage publication boundary.
func WithUsageTTFT(ctx context.Context, duration time.Duration, observed bool) context.Context {
	return context.WithValue(ctx, usageKey{}, usageTTFT{duration, observed})
}

func UsageTTFT(ctx context.Context) (time.Duration, bool) {
	if ctx == nil {
		return 0, false
	}
	v, _ := ctx.Value(usageKey{}).(usageTTFT)
	return v.duration, v.observed
}
