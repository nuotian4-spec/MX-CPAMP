package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

type kiloLatencyFlushWriter struct {
	*httptest.ResponseRecorder
	flushErr, writeErr error
}

func TestKiloLatencyApplicationLogVisibility(t *testing.T) {
	logger := log.StandardLogger()
	oldOut, oldLevel, oldFormatter := logger.Out, logger.Level, logger.Formatter
	oldHooks := logger.ReplaceHooks(make(log.LevelHooks))
	var output bytes.Buffer
	logger.SetOutput(&output)
	logger.SetLevel(log.DebugLevel)
	logger.SetFormatter(&logging.LogFormatter{})
	defer func() {
		logger.SetOutput(oldOut)
		logger.SetLevel(oldLevel)
		logger.SetFormatter(oldFormatter)
		logger.ReplaceHooks(oldHooks)
	}()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request = c.Request.WithContext(logging.WithRequestID(c.Request.Context(), "existing-latency-request"))
	finish := ObserveKiloHTTP(c)
	state := kilolatency.From(c.Request.Context())
	state.Enable(true)
	state.Mark("sent")
	state.Mark("text")
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = c.Writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"private-answer\"}\n\n"))
	c.Writer.Flush()
	finish()
	finish()
	text := output.String()
	for _, key := range []string{`"server_total_ms":`, `"cpa_first_text_ms":`, `"outcome":"success_text"`, `"request_id":"existing-latency-request"`} {
		if !strings.Contains(text, key) {
			t.Fatalf("actual application formatter lost %s: %s", key, text)
		}
	}
	if strings.Contains(text, "private-answer") || strings.Count(text, "Kilo request latency") != 1 {
		t.Fatalf("unsafe/duplicate summary: %s", text)
	}
}

func (w *kiloLatencyFlushWriter) FlushError() error { return w.flushErr }
func (w *kiloLatencyFlushWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(p)
}

func TestKiloLatencyWriterRequiresBodyAndSuccessfulFlush(t *testing.T) {
	for _, mode := range []string{"success", "write_failure", "flush_failure", "heartbeat", "terminal", "nonstream"} {
		t.Run(mode, func(t *testing.T) {
			_, state := kilolatency.NewContext(context.Background())
			state.Enable(mode != "nonstream")
			state.Mark("sent")
			state.Mark("text")
			transport := &kiloLatencyFlushWriter{ResponseRecorder: httptest.NewRecorder()}
			if mode == "write_failure" {
				transport.writeErr = errors.New("write failed")
			}
			if mode == "flush_failure" {
				transport.flushErr = errors.New("flush failed")
			}
			c, _ := gin.CreateTestContext(transport)
			writer := &kiloLatencyWriter{ResponseWriter: c.Writer, state: state}
			writer.Header().Set("Content-Type", "text/event-stream")
			chunk := `data: {"type":"response.output_text.delta","delta":"answer"}` + "\n\n"
			if mode == "heartbeat" {
				chunk = ": keepalive\n\n"
			}
			if mode == "terminal" {
				chunk = `data: {"type":"response.completed","response":{"output_text":"answer"}}` + "\n\n"
			}
			if mode == "nonstream" {
				writer.Header().Set("Content-Type", "application/json")
			}
			_, _ = writer.Write([]byte(chunk))
			_ = writer.FlushError()
			fields := state.Finish(nil, 200)
			_, has := fields["cpa_first_text_ms"]
			if has != (mode == "success") {
				t.Fatalf("mode %s first-body=%t fields=%v", mode, has, fields)
			}
			if mode == "write_failure" || mode == "flush_failure" {
				if fields["outcome"] != "failed" {
					t.Fatalf("write/flush failure lost: %v", fields)
				}
			}
		})
	}
}
