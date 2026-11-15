package handlers

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
)

func TestKiloPayloadZstdWindowAndOutputLimits(t *testing.T) {
	// A valid frame with a tiny payload and a separately declared 128 MiB window.
	raw, err := hex.DecodeString("28b52ffd0488b900007b226d6f64656c223a226770742d362e312d736f6c227d2a82afeb")
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name, scope string
		trusted     bool
		limit       int64
		window      byte
		wantWindow  bool
		wantOutput  bool
	}{
		{"kilo-window", "kilo", true, 64 << 20, 0x88, true, false},
		{"kilo-at-window", "kilo", true, 64 << 20, 0x80, false, false},
		{"kilo-stricter-output", "kilo", true, 8, 0x80, false, true},
		{"codex-unchanged", "codex", true, 64 << 20, 0x88, false, false},
		{"kilo-no-http-policy", "kilo", false, 64 << 20, 0x88, false, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			frame := bytes.Clone(raw)
			frame[5] = sample.window
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(frame))
			ctx := channelscope.With(c.Request.Context(), sample.scope)
			ctx = channelscope.WithKiloPayload(ctx, sample.trusted)
			c.Request = c.Request.WithContext(ctx)
			c.Request.Header.Set("Content-Encoding", "zstd")
			c.Set(MaxRequestBodyBytesContextKey, sample.limit)
			body, err := ReadRequestBody(c)
			if sample.wantWindow || sample.wantOutput {
				var size *http.MaxBytesError
				if !errors.As(err, &size) || size.Limit != sample.limit {
					t.Fatalf("output limit=%v", err)
				}
			} else if err != nil || string(body) != `{"model":"gpt-6.1-sol"}` {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}
