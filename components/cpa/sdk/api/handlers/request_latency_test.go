package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/httpwire"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	log "github.com/sirupsen/logrus"
)

func requestLatencyLog(t *testing.T, level log.Level) *bytes.Buffer {
	t.Helper()
	logger := log.StandardLogger()
	oldOut, oldLevel, oldFormatter := logger.Out, logger.Level, logger.Formatter
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	output := &bytes.Buffer{}
	logger.SetOutput(output)
	logger.SetLevel(level)
	logger.SetFormatter(&logging.LogFormatter{})
	t.Cleanup(func() {
		logger.SetOutput(oldOut)
		logger.SetLevel(oldLevel)
		logger.SetFormatter(oldFormatter)
		logger.ReplaceHooks(oldHooks)
	})
	return output
}

func TestRequestLatencyHTTPDirectRoute(t *testing.T) {
	output := requestLatencyLog(t, log.DebugLevel)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = c.Request.WithContext(logging.WithRequestID(c.Request.Context(), "existing-request"))
	finish := ObserveKiloHTTP(c)
	state := requestlatency.From(c.Request.Context())
	if state == nil {
		t.Fatal("missing observation")
	}
	host := &handlerDirectExecutorRouteHost{}
	host.hasRouters = true
	host.route = func(context.Context, pluginapi.ModelRouteRequest) (pluginapi.ModelRouteResponse, bool) {
		return pluginapi.ModelRouteResponse{Handled: true, TargetKind: pluginapi.ModelRouteTargetExecutor, Target: "private-plugin"}, true
	}
	host.stream = func(ctx context.Context, _ string, req coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
		if requestlatency.From(ctx) != state {
			t.Fatal("context dropped at direct execution")
		}
		chunks := make(chan coreexecutor.StreamChunk, 2)
		chunks <- coreexecutor.StreamChunk{Payload: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"private-")}
		chunks <- coreexecutor.StreamChunk{Payload: []byte("answer\"}}]}\n\n")}
		close(chunks)
		return &coreexecutor.StreamResult{Chunks: chunks}, nil
	}
	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	handler.SetModelRouterHost(host)
	raw := []byte("{\"model\":\"model\",\"messages\":[{\"role\":\"user\",\"content\":\"secret-prompt\"}]}")
	data, _, failures := handler.ExecuteStreamWithAuthManager(c.Request.Context(), "openai", "model", raw, "")
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	for chunk := range data {
		if _, err := c.Writer.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	for failure := range failures {
		if failure != nil {
			t.Fatalf("stream failed: %v", failure)
		}
	}
	finish()
	finish()
	text := output.String()
	for _, expected := range []string{"\"route\":\"direct_plugin\"", "\"admission_mode\":\"unobserved\"", "\"host_first_output_ms\":", "\"execution_input_bytes\":", "\"request_id\":\"existing-request\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s: %s", expected, text)
		}
	}
	if strings.Count(text, "CPA request stages") != 1 {
		t.Fatalf("summary count: %s", text)
	}
	for _, forbidden := range []string{"private-answer", "secret-prompt", "private-plugin", "credential_wait_ms", "selection_lock_wait_ms"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unexpected %s: %s", forbidden, text)
		}
	}
}

func TestRequestLatencyHTTPGatesAndCancellation(t *testing.T) {
	for _, mode := range []string{"disabled", "kilo", "canceled", "failed"} {
		t.Run(mode, func(t *testing.T) {
			level := log.DebugLevel
			if mode == "disabled" {
				level = log.InfoLevel
			}
			output := requestLatencyLog(t, level)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			finish := ObserveKiloHTTP(c)
			state := requestlatency.From(c.Request.Context())
			if mode == "disabled" && state != nil {
				t.Fatal("debug off creates observation state")
			}
			if mode == "kilo" {
				kilolatency.From(c.Request.Context()).Enable(true)
			}
			if mode == "canceled" {
				cancel()
			}
			if mode == "failed" {
				c.Writer.WriteHeader(502)
			}
			finish()
			finish()
			text := output.String()
			want := 1
			if mode == "disabled" || mode == "kilo" {
				want = 0
			}
			if strings.Count(text, "CPA request stages") != want {
				t.Fatalf("summary gate=%s", text)
			}
			if mode == "canceled" && !strings.Contains(text, "\"outcome\":\"canceled\"") {
				t.Fatal(text)
			}
			if mode == "failed" && !strings.Contains(text, "\"outcome\":\"failed\"") {
				t.Fatal(text)
			}
			if strings.Contains(text, "execution_input_bytes") {
				t.Fatal("unknown input length emitted")
			}
		})
	}
}

type requestLatencyTransport struct {
	*httptest.ResponseRecorder
	err      error
	short    bool
	flushErr error
}

func (w *requestLatencyTransport) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.short {
		return len(p) - 1, nil
	}
	return w.ResponseRecorder.Write(p)
}
func (w *requestLatencyTransport) FlushError() error { return w.flushErr }

func TestRequestLatencyWriterPreservesTransportErrors(t *testing.T) {
	for _, mode := range []string{"flush", "write", "short"} {
		t.Run(mode, func(t *testing.T) {
			requestLatencyLog(t, log.DebugLevel)
			failure := errors.New("private-transport-error")
			transport := &requestLatencyTransport{ResponseRecorder: httptest.NewRecorder()}
			if mode == "flush" {
				transport.flushErr = failure
			}
			if mode == "write" {
				transport.err = failure
			}
			if mode == "short" {
				transport.short = true
			}
			c, _ := gin.CreateTestContext(transport)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			ObserveKiloHTTP(c)
			state := requestlatency.From(c.Request.Context())
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			payload := []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"private-answer\"}\n\n")
			n, err := c.Writer.Write(payload)
			if mode == "write" && !errors.Is(err, failure) {
				t.Fatalf("write error=%v", err)
			}
			if mode == "short" && (err != nil || n != len(payload)-1) {
				t.Fatalf("short write altered: %d/%v", n, err)
			}
			flushErr := httpwire.FlushResponse(c.Writer)
			if mode == "flush" && !errors.Is(flushErr, failure) {
				t.Fatalf("flush error lost: %v", flushErr)
			}
			fields := state.Finish(nil, 200)
			if fields["outcome"] != "failed" {
				t.Fatalf("error not recorded: %v", fields)
			}
			_, hasOutput := fields["host_first_output_ms"]
			if hasOutput != (mode == "flush") {
				t.Fatalf("successful-write observation=%v", fields)
			}
		})
	}
}
