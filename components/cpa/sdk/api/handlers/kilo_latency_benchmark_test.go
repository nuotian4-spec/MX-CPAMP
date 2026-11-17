package handlers

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

// BenchmarkKiloLatencyObservation compares one identical in-memory SSE request.
// It includes entry, body recognition, actual writes/flushes, and summary setup.
func BenchmarkKiloLatencyObservation(b *testing.B) {
	oldMode, oldLevel := gin.Mode(), log.GetLevel()
	logger := log.StandardLogger()
	oldOutput, oldFormatter := logger.Out, logger.Formatter
	logger.SetOutput(io.Discard)
	logger.SetFormatter(&logging.LogFormatter{})
	gin.SetMode(gin.TestMode)
	log.SetLevel(log.InfoLevel)
	defer func() {
		gin.SetMode(oldMode)
		log.SetLevel(oldLevel)
		logger.SetOutput(oldOutput)
		logger.SetFormatter(oldFormatter)
	}()
	events := [][]byte{
		[]byte(`{"type":"response.created","response":{"id":"fixture"}}`),
		[]byte(`{"type":"response.reasoning_summary_text.delta","delta":"reasoning"}`),
		[]byte(`{"type":"response.output_text.delta","delta":"answer"}`),
		[]byte(`{"type":"response.completed","response":{"status":"completed"}}`),
	}
	frames := make([][]byte, len(events))
	for i, event := range events {
		frames[i] = append(append([]byte("data: "), event...), []byte("\n\n")...)
	}
	for _, mode := range []struct {
		name           string
		enabled, debug bool
	}{{"baseline", false, false}, {"observed", true, false}, {"observed_debug", true, true}} {
		b.Run(mode.name, func(b *testing.B) {
			log.SetLevel(log.InfoLevel)
			if mode.debug {
				log.SetLevel(log.DebugLevel)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				var finish func()
				var state *kilolatency.State
				if mode.enabled {
					finish = ObserveKiloHTTP(c)
					state = kilolatency.From(c.Request.Context())
					state.Enable(true)
					state.Mark("sent")
				}
				c.Writer.Header().Set("Content-Type", "text/event-stream")
				for index, frame := range frames {
					if mode.enabled {
						if text, _ := kilolatency.Content(events[index]); text {
							state.Mark("text")
						}
					}
					_, _ = c.Writer.Write(frame)
					c.Writer.Flush()
				}
				if finish != nil {
					finish()
				}
			}
		})
	}
}
