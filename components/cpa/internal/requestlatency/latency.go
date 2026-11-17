// Package requestlatency observes host execution without changing request behavior.
package requestlatency

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
)

type contextKey struct{}
type routeKey struct{}
type outputKey struct{}

// Stage is an allowlist; callers cannot publish arbitrary field names.
type Stage uint8

const (
	SelectionLockWait Stage = iota
	CredentialWait
	HostPrepare
	RequestClone
	RPCSerialize
	PluginCallReturn
	PluginFirstOutput
	NestedPluginFirstOutput
	BPSPrepare
	BPSUpload
	stageCount
)

var stageNames = [...]string{
	"selection_lock_wait", "credential_wait", "host_prepare",
	"request_clone", "rpc_serialize", "plugin_call_return",
	"plugin_first_output", "nested_plugin_first_output",
	"bps_prepare", "bps_upload",
}

type Route uint8

const (
	DirectPlugin Route = iota
	CredentialExecutor
)

// State contains metadata only. Missing stages remain absent, including Home admission.
type State struct {
	mu                           sync.Mutex
	now                          func() time.Time
	entry, output                time.Time
	ended, failed, canceled      bool
	route, admission, outputKind string
	inputBytes                   int
	inputKnown                   bool
	executors                    [3]int
	nested                       [2]int
	totals                       [stageCount]time.Duration
	counts                       [stageCount]int
}

func NewContext(ctx context.Context) (context.Context, *State) {
	if s := From(ctx); s != nil {
		return ctx, s
	}
	s := &State{now: time.Now, entry: time.Now(), route: "unknown", admission: "unobserved"}
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

func WithRoute(ctx context.Context, route Route, nested, home bool, inputBytes int) context.Context {
	s := From(ctx)
	if s == nil || route > CredentialExecutor {
		return ctx
	}
	s.mu.Lock()
	if !s.ended {
		if nested {
			s.nested[route]++
		} else {
			s.route = [...]string{"direct_plugin", "credential_executor"}[route]
			s.inputBytes = inputBytes
			s.inputKnown = true
		}
		if route == CredentialExecutor {
			mode := "local"
			if home {
				mode = "home"
			}
			if s.admission != "unobserved" && s.admission != mode {
				mode = "mixed"
			}
			s.admission = mode
		}
	}
	s.mu.Unlock()
	return context.WithValue(ctx, routeKey{}, nested)
}

// Executor classifies an actual host entry without exposing arbitrary IDs.
func (s *State) Executor(pluginID string, builtinBPS bool) {
	if s == nil {
		return
	}
	index := 1
	if builtinBPS {
		index = 2
	} else if pluginID == "cpa-bps" {
		index = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.executors[index]++
	}
}

func (s *State) add(stage Stage, elapsed time.Duration) {
	if s == nil || stage >= stageCount {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.totals[stage] += elapsed
		s.counts[stage]++
	}
}

func (s *State) Measure(stage Stage) func() {
	if s == nil || stage >= stageCount {
		return func() {}
	}
	start := s.now()
	return func() { s.add(stage, s.now().Sub(start)) }
}

func (s *State) Failure(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.failed = true
		s.canceled = s.canceled || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}
}

func (s *State) Output(kind string) {
	if s == nil || (kind != "text" && kind != "tool") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended && s.output.IsZero() {
		s.output, s.outputKind = s.now(), kind
	}
}

func (s *State) Finish(err error, status int) map[string]any {
	if s == nil {
		return nil
	}
	s.Failure(err)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return nil
	}
	s.ended = true
	outcome := "completed"
	if s.failed || status >= 400 {
		outcome = "failed"
	}
	if s.canceled {
		outcome = "canceled"
	}
	fields := map[string]any{
		"route": s.route, "admission_mode": s.admission, "outcome": outcome,
		"server_total_ms": float64(s.now().Sub(s.entry)) / float64(time.Millisecond),
	}
	if s.inputKnown {
		fields["execution_input_bytes"] = s.inputBytes
	}
	for i, name := range [...]string{"executor_cpa_bps_count", "executor_other_plugin_count", "executor_builtin_bps_count"} {
		if s.executors[i] > 0 {
			fields[name] = s.executors[i]
		}
	}
	if !s.output.IsZero() {
		fields["host_first_output_ms"] = float64(s.output.Sub(s.entry)) / float64(time.Millisecond)
		fields["host_first_output_kind"] = s.outputKind
	}
	for stage, count := range s.counts {
		if count > 0 {
			fields[stageNames[stage]+"_ms"] = float64(s.totals[stage]) / float64(time.Millisecond)
			fields[stageNames[stage]+"_count"] = count
		}
	}
	if s.nested[DirectPlugin] > 0 {
		fields["nested_direct_plugin_count"] = s.nested[DirectPlugin]
	}
	if s.nested[CredentialExecutor] > 0 {
		fields["nested_credential_executor_count"] = s.nested[CredentialExecutor]
	}
	return fields
}

// StartPlugin distinguishes function return from observed output on its existing consumer.
func StartPlugin(ctx context.Context) context.Context {
	s := From(ctx)
	if s == nil {
		return ctx
	}
	start := s.now()
	stage := PluginFirstOutput
	if nested, _ := ctx.Value(routeKey{}).(bool); nested {
		stage = NestedPluginFirstOutput
	}
	output := &Output{found: func(string) { s.add(stage, s.now().Sub(start)) }}
	return context.WithValue(ctx, outputKey{}, output)
}

func PluginOutput(ctx context.Context, raw []byte) {
	if ctx == nil {
		return
	}
	if output, _ := ctx.Value(outputKey{}).(*Output); output != nil {
		output.Write(raw)
	}
}

// Output observes complete SSE/JSON events across chunk boundaries, bounded to 1 MiB.
// It neither reads a stream nor emits, modifies, or stores payloads in a summary.
type Output struct {
	pending []byte
	done    bool
	found   func(string)
}

func NewOutput(found func(string)) *Output { return &Output{found: found} }

func (o *Output) observe(raw []byte) bool {
	text, tool := kilolatency.Content(raw)
	if !text && !tool {
		return false
	}
	o.done, o.pending = true, nil
	kind := "text"
	if !text {
		kind = "tool"
	}
	if o.found != nil {
		o.found(kind)
	}
	return true
}

func (o *Output) Write(raw []byte) {
	if o == nil || o.done {
		return
	}
	const maxEventBytes = 1 << 20
	if len(o.pending)+len(raw) > maxEventBytes {
		o.done, o.pending = true, nil
		return
	}
	o.pending = append(o.pending, raw...)
	for {
		end, width := bytes.Index(o.pending, []byte("\n\n")), 2
		if cr := bytes.Index(o.pending, []byte("\r\n\r\n")); cr >= 0 && (end < 0 || cr < end) {
			end, width = cr, 4
		}
		if end < 0 {
			trimmed := bytes.TrimSpace(o.pending)
			if len(trimmed) > 0 && trimmed[0] == '{' && ojson.ValidBytes(trimmed) {
				o.observe(trimmed)
				o.pending = nil
			}
			return
		}
		var event []byte
		for _, line := range bytes.Split(o.pending[:end], []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if bytes.HasPrefix(line, []byte("data:")) {
				event = append(event, bytes.TrimPrefix(line[5:], []byte(" "))...)
				event = append(event, '\n')
			}
		}
		o.pending = o.pending[end+width:]
		if o.observe(event) {
			return
		}
		if len(o.pending) == 0 {
			o.pending = nil
			return
		}
	}
}
