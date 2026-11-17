package handlers

import (
	"io"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/httpwire"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

// ObserveKiloHTTP starts at the HTTP handler entrance, before reading the body.
// Non-Kilo requests never enable the observer or emit a diagnostic summary.
func ObserveKiloHTTP(c *gin.Context) func() {
	ctx, state := kilolatency.NewContext(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	writer := &kiloLatencyWriter{ResponseWriter: c.Writer, state: state}
	c.Writer = writer
	return func() {
		if fields := state.Finish(c.Request.Context().Err(), c.Writer.Status()); fields != nil && log.IsLevelEnabled(log.DebugLevel) {
			fields["request_id"] = logging.GetRequestID(ctx)
			fields["protocol"] = c.Request.Proto
			// The existing application formatter only prints allowlisted fields.
			// Keep the complete safe snapshot in the same single diagnostic entry.
			doc := ojson.NewDocString("{}")
			for name, value := range fields {
				_ = doc.Set(name, value)
			}
			log.WithFields(log.Fields(fields)).Debugf("Kilo request latency %s", doc.String())
		}
	}
}

type kiloLatencyWriter struct {
	gin.ResponseWriter
	state       *kilolatency.State
	output      kilolatency.Output
	flushed     bool
	writeFailed bool
}

func (w *kiloLatencyWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if w.state.Enabled() {
		if err != nil || n != len(data) {
			w.writeFailed = true
			if err == nil {
				w.state.Failure(io.ErrShortWrite)
			} else {
				w.state.Failure(err)
			}
		} else if !w.flushed && strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
			w.output.Write(data[:n])
		}
	}
	return n, err
}

func (w *kiloLatencyWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func (w *kiloLatencyWriter) FlushError() error {
	err := httpwire.FlushResponse(w.ResponseWriter)
	if err != nil {
		w.state.Failure(err)
		return err
	}
	if !w.writeFailed && w.output.Text && !w.flushed {
		w.flushed = true
		w.state.Mark("flushed")
	}
	return nil
}

func (w *kiloLatencyWriter) Flush() {
	if !w.state.Enabled() {
		w.ResponseWriter.Flush()
		return
	}
	_ = w.FlushError()
}
