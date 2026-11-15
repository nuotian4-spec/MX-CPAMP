package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type bpsCancellationResult struct {
	result coreauth.Result
	ctxErr error
}

type bpsCancellationHook struct {
	coreauth.NoopHook
	results chan bpsCancellationResult
}

func (h *bpsCancellationHook) OnResult(ctx context.Context, result coreauth.Result) {
	h.results <- bpsCancellationResult{result: result, ctxErr: ctx.Err()}
}

func (h *bpsCancellationHook) await(t *testing.T) {
	t.Helper()
	select {
	case observed := <-h.results:
		if observed.ctxErr != nil || observed.result.Success || observed.result.PermissionDenial == nil {
			t.Fatalf("invalid cancellation result: context=%v result=%+v", observed.ctxErr, observed.result)
		}
		if observed.result.Error.Code != coreauth.ErrorCodeRequestScoped {
			t.Fatalf("confirmed refusal did not preserve stop policy: %+v", observed.result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("confirmed denial was not committed while downstream remained canceled")
	}
}

// This decorator changes delivery order only after the real executor has
// parsed the upstream refusal. It never manufactures permission evidence.
type bpsCancelAfterDenialExecutor struct {
	*executor.BPSExecutor
	t        *testing.T
	cancel   context.CancelFunc
	boundary string
}

func (e *bpsCancelAfterDenialExecutor) confirm(err error) {
	e.t.Helper()
	var evidence interface{ BPSPermissionDenial() (string, int, bool) }
	if !errors.As(err, &evidence) {
		e.t.Fatalf("native denial evidence missing: %v", err)
	}
	source, status, _ := evidence.BPSPermissionDenial()
	if source != "http" && source != "inband" || status != 200 && status != 403 {
		e.t.Fatalf("unexpected denial provenance: %q %d", source, status)
	}
	if source == "inband" {
		var accepted interface{ IsStreamTruncated() bool }
		if !errors.As(err, &accepted) || !accepted.IsStreamTruncated() {
			e.t.Fatalf("native accepted-response classification lost: %v", err)
		}
	}
}

func (e *bpsCancelAfterDenialExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	result, err := e.BPSExecutor.Execute(ctx, auth, req, opts)
	e.confirm(err)
	e.cancel()
	return result, err
}

func (e *bpsCancelAfterDenialExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	result, err := e.BPSExecutor.ExecuteStream(ctx, auth, req, opts)
	if err != nil {
		e.confirm(err)
		e.cancel()
		return result, err
	}
	var terminal coreexecutor.StreamChunk
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			terminal = chunk
		}
	}
	e.confirm(terminal.Err)
	if e.boundary == "bootstrap" {
		out := make(chan coreexecutor.StreamChunk, 1)
		out <- terminal
		close(out)
		e.cancel()
		return &coreexecutor.StreamResult{Headers: result.Headers, Chunks: out}, nil
	}
	out := make(chan coreexecutor.StreamChunk)
	go func() {
		defer close(out)
		payload := coreexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"response.created\"}\n\n")}
		out <- payload // Bootstrap must receive before cancellation.
		if e.boundary == "remaining" {
			out <- payload // The first payload has already reached the client.
			e.cancel()
		} else {
			e.cancel()
			out <- payload // The canceled wrapper must drain through the denial.
		}
		out <- terminal
	}()
	return &coreexecutor.StreamResult{Headers: result.Headers, Chunks: out}, nil
}

func TestBPSPermissionConfirmedCancellationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status   int
		boundary string
	}{
		{403, "execute"}, {403, "stream"},
		{200, "execute"}, {200, "bootstrap"}, {200, "drain"}, {200, "remaining"},
	} {
		t.Run(fmt.Sprintf("http=%d/%s", tc.status, tc.boundary), func(t *testing.T) {
			cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
			cfg.BPS.Models = []string{"cancel-model"}
			hook := &bpsCancellationHook{results: make(chan bpsCancellationResult, 4)}
			manager := coreauth.NewManager(nil, nil, hook)
			manager.SetConfig(cfg)
			manager.SetRetryConfig(2, 0, 0)
			store := coreauth.NewFileCooldownStateStore(t.TempDir())
			manager.SetCooldownStateStore(store)
			auth := &coreauth.Auth{ID: "cancel-native", Provider: "bps", Status: coreauth.StatusActive, Attributes: map[string]string{"runtime_only": "true"}, Metadata: map[string]any{"access_token": "synthetic-token", "account_id": "synthetic-account", "sub": "synthetic-subject"}}
			if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
				t.Fatal(err)
			}
			registry.GetGlobalRegistry().RegisterClient(auth.ID, "bps", []*registry.ModelInfo{{ID: "cancel-model", Type: "bps", OwnedBy: "bps", UserDefined: true}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
			posts := 0
			manager.SetRoundTripperProvider(bpsDiagnosticTransport(func(*http.Request) (*http.Response, error) {
				posts++
				wire := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_cancel\",\"status\":\"failed\",\"error\":{\"status\":403,\"code\":\"permission_denied\"}}}\n\n"
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			native := executor.NewBPSExecutor(cfg)
			manager.RegisterExecutor(&bpsCancelAfterDenialExecutor{BPSExecutor: native, t: t, cancel: cancel, boundary: tc.boundary})
			req := coreexecutor.Request{Model: "cancel-model", Payload: []byte(`{"model":"cancel-model","input":"synthetic"}`)}
			opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: req.Payload}
			before, _ := manager.GetByID(auth.ID)
			if tc.boundary == "execute" {
				_, _ = manager.Execute(ctx, []string{"bps"}, req, opts)
				hook.await(t)
			} else {
				result, _ := manager.ExecuteStream(ctx, []string{"bps"}, req, opts)
				if result != nil {
					if tc.boundary == "remaining" {
						<-result.Chunks
					}
					<-ctx.Done()
					// No output receiver is present until the denial is committed:
					// payload forwarding must select cancellation and drain the source.
					hook.await(t)
					for range result.Chunks {
					}
				} else {
					hook.await(t)
				}
			}
			state, _ := manager.GetByID(auth.ID)
			if ctx.Err() == nil || posts != 1 || len(state.PermissionSuspensions) != 1 {
				t.Fatalf("confirmed denial lost: canceled=%v posts=%d suspensions=%+v", ctx.Err(), posts, state.PermissionSuspensions)
			}
			if state.Generation != before.Generation+1 {
				t.Fatalf("denial recorded more than once: generation=%d before=%d", state.Generation, before.Generation)
			}
			if len(hook.results) != 0 {
				t.Fatal("duplicate denial result")
			}
			denial := state.PermissionSuspensions[0]
			if denial.UpstreamHTTPStatus != tc.status || denial.SemanticStatus != 403 {
				t.Fatalf("provenance changed: %+v", denial)
			}
			manager.RegisterExecutor(native)
			_, _ = manager.Execute(context.Background(), []string{"bps"}, req, opts)
			if posts != 1 {
				t.Fatal("following request reached suspended upstream")
			}
			restarted := coreauth.NewManager(nil, nil, nil)
			restarted.SetCooldownStateStore(store)
			if _, err := restarted.Register(coreauth.WithSkipPersist(context.Background()), auth); err != nil {
				t.Fatal(err)
			}
			if err := restarted.RestoreCooldownStates(context.Background()); err != nil {
				t.Fatal(err)
			}
			restored, _ := restarted.GetByID(auth.ID)
			if len(restored.PermissionSuspensions) != 1 {
				t.Fatal("cancellation prevented durable suspension")
			}
			t.Logf("native=true http=%d boundary=%s posts=1 next_posts=0 suspension=1 restart=1 generation_delta=1", tc.status, tc.boundary)
		})
	}
}
