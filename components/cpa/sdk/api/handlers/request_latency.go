package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/httpwire"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
	log "github.com/sirupsen/logrus"
)

// observeRequestHTTP shares the existing HTTP lifecycle without enabling Kilo sampling.
func observeRequestHTTP(c *gin.Context) func(bool) {
	if !log.IsLevelEnabled(log.DebugLevel) || channelscope.From(c.Request.Context()) == "kilo" || requestlatency.From(c.Request.Context()) != nil {
		return func(bool) {}
	}
	ctx, state := requestlatency.NewContext(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	writer := &requestLatencyWriter{ResponseWriter: c.Writer, state: state, output: requestlatency.NewOutput(state.Output)}
	c.Writer = writer
	return func(kilo bool) {
		if kilo {
			return
		}
		fields := state.Finish(c.Request.Context().Err(), c.Writer.Status())
		if fields == nil || !log.IsLevelEnabled(log.DebugLevel) {
			return
		}
		fields["request_id"] = logging.GetRequestID(ctx)
		doc := ojson.NewDocString("{}")
		for name, value := range fields {
			_ = doc.Set(name, value)
		}
		log.WithFields(log.Fields(fields)).Debugf("CPA request stages %s", doc.String())
	}
}

type requestLatencyWriter struct {
	gin.ResponseWriter
	state  *requestlatency.State
	output *requestlatency.Output
}

func (w *requestLatencyWriter) Write(raw []byte) (int, error) {
	n, err := w.ResponseWriter.Write(raw)
	if err != nil {
		w.state.Failure(err)
	} else if n != len(raw) {
		w.state.Failure(io.ErrShortWrite)
	}
	if err == nil && n == len(raw) && n > 0 && strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		w.output.Write(raw[:n])
	}
	return n, err
}

func (w *requestLatencyWriter) WriteString(raw string) (int, error) { return w.Write([]byte(raw)) }

func (w *requestLatencyWriter) FlushError() error {
	err := httpwire.FlushResponse(w.ResponseWriter)
	w.state.Failure(err)
	return err
}

func (w *requestLatencyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
