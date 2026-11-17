package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const kiloPayloadLimit = 20 << 20

var kiloPayloadImageOnce sync.Once
var kiloPayloadImageURL string
var kiloPayloadImageRaw []byte

func kiloPayloadLargeImage(t *testing.T) string {
	t.Helper()
	kiloPayloadImageOnce.Do(func() {
		img := image.NewNRGBA(image.Rect(0, 0, 2800, 2100))
		seed := uint32(37)
		for y := 0; y < 2100; y++ {
			for x := 0; x < 2800; x++ {
				seed = seed*1664525 + 1013904223
				img.SetNRGBA(x, y, color.NRGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255})
			}
		}
		for y := 20; y < 220; y++ {
			for x := 20; x < 1250; x++ {
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
		d := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13}
		for y := 40; y < 210; y += 18 {
			d.Dot = fixed.P(30, y)
			d.DrawString("Small text 0123456789 ABC xyz: preserve image details / request size 20 MiB")
		}
		var b bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		kiloPayloadImageRaw = b.Bytes()
		kiloPayloadImageURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	})
	return kiloPayloadImageURL
}

func kiloPayloadBody(protocol string, stream bool, text, imageURL string) string {
	doc := ojson.NewDocString(`{"model":"gpt-6.1-sol"}`)
	_ = doc.Set("stream", stream)
	if protocol == "chat/completions" {
		content := []any{map[string]any{"type": "text", "text": text}}
		if imageURL != "" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL, "detail": "high"}})
		}
		_ = doc.Set("messages", []any{map[string]any{"role": "user", "content": content}})
	} else {
		content := []any{map[string]any{"type": "input_text", "text": text}}
		if imageURL != "" {
			content = append(content, map[string]any{"type": "input_image", "image_url": imageURL, "detail": "high"})
		}
		_ = doc.Set("input", []any{map[string]any{"role": "user", "content": content}})
	}
	return doc.String()
}

func kiloPayloadEnable(t *testing.T, f *kiloNativeFixture, enabled bool) {
	next := f.cfg.CloneForRuntime()
	next.Kilo.ImageReductionEnabled = enabled
	next.SDKConfig.NonStreamKeepAliveInterval = 1
	next.SDKConfig.Streaming.KeepAliveSeconds = 1
	next.SDKConfig.Streaming.SendKeepAliveBeforeFT = true
	f.cfg = next
	f.manager.SetConfig(next)
	f.executor.UpdateConfig(next)
	f.server.Close()
	server := api.NewServer(next, f.manager, access.NewManager(), f.configPath)
	f.server = httptest.NewServer(server.Handler())
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(f.server.Close)
}

func TestKiloPayloadHTTPExactFinalBoundary(t *testing.T) {
	var tickerCalls atomic.Int32
	restore := handlers.SetStreamingKeepAliveTickerFactoryForTest(func(time.Duration) (<-chan time.Time, func()) {
		tickerCalls.Add(1)
		return nil, func() {}
	})
	defer restore()
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
				kiloPayloadEnable(t, f, true)
				status, data := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", kiloPayloadBody(protocol, stream, "", ""))
				if status != 200 {
					t.Fatalf("probe=%d %s", status, data)
				}
				fixedBytes := len(<-f.requests)
				success := 1
				for _, delta := range []int{-1, 0, 1} {
					tickersBefore := tickerCalls.Load()
					bindingsBefore := kiloPayloadBindingFiles(t, f)
					body := kiloPayloadBody(protocol, stream, strings.Repeat("x", kiloPayloadLimit+delta-fixedBytes), "")
					status, data = f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", body)
					if delta > 0 {
						if tickerCalls.Load() != tickersBefore {
							t.Fatal("local rejection started keepalive")
						}
						if !maps.Equal(bindingsBefore, kiloPayloadBindingFiles(t, f)) {
							t.Fatal("local rejection wrote session bindings")
						}
						if status != 413 || !bytes.Contains(data, []byte("request_too_large")) {
							t.Fatalf("oversize=%d %s", status, data)
						}
					} else {
						if status != 200 {
							t.Fatalf("delta=%d HTTP=%d %s", delta, status, data)
						}
						wire := <-f.requests
						if len(wire) != kiloPayloadLimit+delta {
							t.Fatalf("wire=%d want=%d", len(wire), kiloPayloadLimit+delta)
						}
						success++
					}
					f.assertReleased(t, success)
					if int(f.posts.Load()) != success {
						t.Fatalf("local reject attempted POST=%d successes=%d", f.posts.Load(), success)
					}
				}
				t.Logf("final bytes %d/%d accepted; %d rejected; POST=%d", kiloPayloadLimit-1, kiloPayloadLimit, kiloPayloadLimit+1, f.posts.Load())
			})
		}
	}
}

func TestKiloPayloadHTTPImageReductionAndFinalRules(t *testing.T) {
	imageURL := kiloPayloadLargeImage(t)
	for _, protocol := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
				source := kiloPayloadBody(protocol, stream, "preserve-private-text", imageURL)
				status, data := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", source)
				if status != 413 || f.posts.Load() != 0 || !bytes.Contains(data, []byte("request_too_large")) {
					t.Fatalf("default off=%d POST=%d %s", status, f.posts.Load(), data)
				}
				f.assertReleased(t, 0)
				kiloPayloadEnable(t, f, true)
				status, data = f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", source)
				if status != 200 {
					t.Fatalf("reduction=%d %s", status, data)
				}
				wire := <-f.requests
				value := ojson.GetBytes(wire, "input.0.content.1.image_url").String()
				if value == imageURL || len(wire) > 19<<20 || ojson.GetBytes(wire, "input.0.content.1.detail").String() != "high" || ojson.GetBytes(wire, "input.0.content.0.text").String() != "preserve-private-text" {
					t.Fatalf("reduction/fidelity failed bytes=%d", len(wire))
				}
				if dir := os.Getenv("KILO_PAYLOAD_EVIDENCE"); dir != "" && protocol == "responses" && !stream {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					raw, _ := base64.StdEncoding.DecodeString(strings.SplitN(value, ",", 2)[1])
					if err := os.WriteFile(filepath.Join(dir, "small-text-original.png"), kiloPayloadImageRaw, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "small-text-reduced.jpg"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Logf("image input=%d final=%d delta=%d", len(source), len(wire), len(source)-len(wire))
				next := f.cfg.CloneForRuntime()
				matchPath := "input.0.content.1.image_url"
				next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*", Match: []map[string]any{{matchPath: imageURL}}}}, Params: map[string]any{"input.0.content.1.image_url": "https://synthetic.invalid/final.png", "input.0.content.1.detail": "low"}}}
				f.executor.UpdateConfig(next)
				status, data = f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", source)
				if status != 200 {
					t.Fatalf("rule=%d %s", status, data)
				}
				wire = <-f.requests
				if ojson.GetBytes(wire, "input.0.content.1.image_url").String() != "https://synthetic.invalid/final.png" || ojson.GetBytes(wire, "input.0.content.1.detail").String() != "low" {
					t.Fatal("original match or final rule lost")
				}
				next.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"input.0.content.1.image_url": imageURL}}}
				f.executor.UpdateConfig(next)
				status, data = f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", source)
				if status != 413 || !bytes.Contains(data, []byte("request_too_large")) {
					t.Fatalf("rule expansion=%d %s", status, data)
				}
				if f.posts.Load() != 2 {
					t.Fatalf("replayed rule expansion POST=%d", f.posts.Load())
				}
				f.assertReleased(t, 2)
			})
		}
	}
}

func TestKiloPayloadHTTPUpstreamRequestErrorsNeutral(t *testing.T) {
	for _, sample := range []struct {
		name, body, code string
		upstream, want   int
	}{
		{"http413", `{"error":{"code":"too_large","message":"private-upstream"}}`, "request_too_large", 413, 413},
		{"encrypted", "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"invalid_encrypted_content\",\"message\":\"private-upstream\"}}}\n\n", "invalid_encrypted_content", 200, 400},
	} {
		for _, protocol := range []string{"responses", "chat/completions"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", sample.name, protocol, stream), func(t *testing.T) {
					f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
						if sample.upstream == 200 {
							w.Header().Set("Content-Type", "text/event-stream")
						}
						w.WriteHeader(sample.upstream)

						_, _ = io.WriteString(w, sample.body)
					})
					status, data := f.request(t, "POST", "/v1/"+protocol, "synthetic-kilo-key", kiloPayloadBody(protocol, stream, "small", ""))
					if status != sample.want || !bytes.Contains(data, []byte(sample.code)) || bytes.Contains(data, []byte("private-upstream")) {
						t.Fatalf("error HTTP=%d %s", status, data)
					}
					f.assertReleased(t, 1)
					if f.posts.Load() != 1 || f.connects.Load() != 1 {
						t.Fatalf("replayed POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
					}
					for _, a := range f.manager.List() {
						if a.Unavailable || !a.NextRetryAfter.IsZero() {
							t.Fatalf("request error cooled account: %+v", a)
						}
					}
					for _, p := range f.executor.KiloProxySnapshot().Proxies {
						if !p.Healthy {
							t.Fatal("request error damaged proxy health")
						}
					}
				})
			}
		}
	}
}

func TestKiloPayloadHTTPCooldownRulesStayNeutral(t *testing.T) {
	for _, sample := range []struct {
		name, body, match string
		upstream, want    int
	}{
		{"http413", `{"error":{"code":"too_large"}}`, "too_large", 413, 413},
		{"encrypted", "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"invalid_encrypted_content\"}}}\n\n", "invalid_encrypted_content", 200, 400},
	} {
		for _, action := range []string{coreauth.RequestScopedActionStopAndCooldown, coreauth.RequestScopedActionContinueAndCooldown} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", sample.name, action, stream), func(t *testing.T) {
					f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
						if sample.upstream == 200 {
							w.Header().Set("Content-Type", "text/event-stream")
						}
						w.WriteHeader(sample.upstream)
						if sample.upstream == 200 && stream {
							_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-cooldown\",\"model\":\"openai/gpt-6.1-sol\",\"status\":\"in_progress\"}}\n\n")
							w.(http.Flusher).Flush()
						}
						_, _ = io.WriteString(w, sample.body)
					})
					for _, a := range f.manager.List() {
						a.Metadata["request_scoped_errors"] = []config.RequestScopedErrorRule{{Status: sample.want, Match: []string{sample.match}, Action: action}}
						if _, err := f.manager.Update(context.Background(), a); err != nil {
							t.Fatal(err)
						}
					}
					status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", kiloPayloadBody("responses", stream, "small", ""))
					wantStatus := sample.want
					if sample.upstream == 200 && stream {
						wantStatus = 200
					}
					if status != wantStatus || (sample.name == "encrypted" && !bytes.Contains(data, []byte("invalid_encrypted_content"))) {
						t.Fatalf("HTTP=%d %s", status, data)
					}
					f.assertReleased(t, 1)
					if f.posts.Load() != 1 || f.connects.Load() != 1 || f.hook.snapshot()[0].Error.Code != coreauth.ErrorCodeForceCooldown {
						t.Fatalf("action not matched or replayed: posts=%d connects=%d results=%+v", f.posts.Load(), f.connects.Load(), f.hook.snapshot())
					}
					for _, a := range f.manager.List() {
						if a.Unavailable || !a.NextRetryAfter.IsZero() || a.Quota.Exceeded {
							t.Fatalf("request error changed account availability: %+v", a)
						}
						for _, state := range a.ModelStates {
							if state.Unavailable || !state.NextRetryAfter.IsZero() || state.Quota.Exceeded {
								t.Fatalf("request error changed model availability: %+v", state)
							}
						}
					}
					for _, p := range f.executor.KiloProxySnapshot().Proxies {
						if !p.Healthy {
							t.Fatal("request error damaged proxy health")
						}
					}
				})
			}
		}
	}
}

type kiloPayloadFileLogger struct {
	*logging.FileRequestLogger
	checks atomic.Int32
}

func (l *kiloPayloadFileLogger) IsEnabled() bool {
	l.checks.Add(1)
	return l.FileRequestLogger.IsEnabled()
}

func TestKiloPayloadHTTPSuccessZstdLogs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("logging=%t", enabled), func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
			dir := t.TempDir()
			logger := &kiloPayloadFileLogger{FileRequestLogger: logging.NewFileRequestLogger(enabled, dir, "", 10)}
			cfg := f.cfg.CloneForRuntime()
			cfg.CommercialMode = false
			cfg.SDKConfig.RequestLog = enabled
			f.server.Close()
			server := api.NewServer(cfg, f.manager, access.NewManager(), f.configPath, api.WithRequestLoggerFactory(func(*config.Config, string) logging.RequestLogger { return logger }))
			f.server = httptest.NewServer(server.Handler())
			f.server.Client().Transport = &http.Transport{Proxy: nil}
			t.Cleanup(f.server.Close)
			marker := "KILO-PRIVATE-ZSTD-SUCCESS-7d3a-plaintext"
			body := kiloPayloadBody("responses", true, strings.Repeat(marker, 40000), "")
			encoder, err := zstd.NewWriter(nil, zstd.WithWindowSize(1<<20))
			if err != nil {
				t.Fatal(err)
			}
			compressed := encoder.EncodeAll([]byte(body), nil)
			encoder.Close()
			req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", bytes.NewReader(compressed))
			req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", "zstd")
			before := logger.checks.Load()
			resp, err := f.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || !bytes.Contains(data, []byte("response.completed")) || !bytes.Contains(data, []byte("kilo-ok")) {
				t.Fatalf("HTTP=%d body=%s", resp.StatusCode, data)
			}
			f.assertReleased(t, 1)
			if f.posts.Load() != 1 || !bytes.Contains(<-f.requests, []byte(marker)) || logger.checks.Load() <= before {
				t.Fatal("request, marker, or logger probe missing")
			}
			files, terminalLogs := 0, 0
			err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				files++
				b, err := os.ReadFile(path)
				if bytes.Contains(b, []byte(marker)) || bytes.Contains(b, compressed[:min(48, len(compressed))]) || bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(compressed))) {
					t.Fatalf("plaintext or compressed request body in log: %s", path)
				}
				if strings.Contains(path, "request-log-parts") {
					t.Fatalf("temporary body file retained: %s", path)
				}
				if bytes.Contains(b, []byte("response.completed")) {
					terminalLogs++
				}
				return err
			})
			if err != nil || (enabled && terminalLogs == 0) || (!enabled && files != 0) {
				t.Fatalf("log evidence: enabled=%t files=%d terminal=%d err=%v", enabled, files, terminalLogs, err)
			}
			t.Logf("POST=1 terminal=1 logger_checks=%d files=%d terminal_logs=%d; request plaintext/compressed/temp files=0", logger.checks.Load()-before, files, terminalLogs)
		})
	}
}

func TestKiloPayloadHTTPInboundLimitsAndLogs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("logging=%t", enabled), func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
			dir := t.TempDir()
			cfg := f.cfg.CloneForRuntime()
			cfg.CommercialMode = false
			cfg.SDKConfig.RequestLog = enabled
			f.server.Close()
			server := api.NewServer(cfg, f.manager, access.NewManager(), f.configPath, api.WithRequestLoggerFactory(func(*config.Config, string) logging.RequestLogger {
				return logging.NewFileRequestLogger(enabled, dir, "", 10)
			}))
			f.server = httptest.NewServer(server.Handler())
			f.server.Client().Transport = &http.Transport{Proxy: nil}
			t.Cleanup(f.server.Close)
			large := kiloPayloadBody("responses", false, strings.Repeat("private-input-", kiloPayloadLimit/14+2), "")
			status, data := f.request(t, "POST", "/backend-api/codex/responses", "synthetic-kilo-key", large)
			if status != 413 || !bytes.Contains(data, []byte("request_too_large")) {
				t.Fatalf("alias=%d %s", status, data)
			}
			decoded := kiloPayloadBody("responses", false, strings.Repeat("z", 65<<20), "")
			encoder, err := zstd.NewWriter(nil, zstd.WithWindowSize(1<<20))
			if err != nil {
				t.Fatal(err)
			}
			compressed := encoder.EncodeAll([]byte(decoded), nil)
			encoder.Close()
			req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", bytes.NewReader(compressed))
			req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
			req.Header.Set("Content-Encoding", "zstd")
			resp, err := f.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 413 {
				t.Fatalf("decoded 64MiB limit=%d %s", resp.StatusCode, data)
			}
			req, _ = http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(decoded))
			req.ContentLength = -1
			req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
			resp, err = f.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 413 {
				t.Fatalf("chunked 64MiB limit=%d %s", resp.StatusCode, data)
			}
			f.assertReleased(t, 0)
			if f.posts.Load() != 0 || f.connects.Load() != 0 {
				t.Fatalf("local refusal attempted upstream")
			}
			err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, err := os.ReadFile(path)
				if bytes.Contains(b, []byte("private-input-")) || bytes.Contains(b, []byte(strings.Repeat("z", 100))) {
					t.Fatalf("request body in log: %s", path)
				}
				if strings.Contains(path, "request-log-parts") {
					t.Fatalf("temporary body file retained: %s", path)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func kiloPayloadBindingFiles(t *testing.T, f *kiloNativeFixture) map[string]string {
	t.Helper()
	result := map[string]string{}
	dir := filepath.Join(config.KiloStateDirectory(f.cfg), "bindings")
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			result[path] = string(raw)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestKiloPayloadHTTPConditionalRulesKeepOriginalView(t *testing.T) {
	imageURL := kiloPayloadLargeImage(t)
	for _, kind := range []string{"match", "not-match", "exist", "not-exist", "filter-query"} {
		t.Run(kind, func(t *testing.T) {
			f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
			kiloPayloadEnable(t, f, true)
			rule := config.PayloadModelRule{Name: "gpt-*"}
			switch kind {
			case "match":
				rule.Match = []map[string]any{{"input.#(role==user).content.#(type==input_image).image_url": imageURL}}
			case "not-match":
				rule.NotMatch = []map[string]any{{"input.0.content.1.image_url": "different"}}
			case "exist":
				rule.Exist = []string{"input.0.content.1.image_url"}
			case "not-exist":
				rule.NotExist = []string{"input.0.content.5"}
			case "filter-query":
				rule.Match = []map[string]any{{"input.0.content.1.image_url": imageURL}}
			}
			cfg := f.cfg.CloneForRuntime()
			cfg.Payload.Override = []config.PayloadRule{{Models: []config.PayloadModelRule{rule}, Params: map[string]any{"input.0.content.1.image_url": "https://synthetic.invalid/conditional.png"}}}
			if kind == "filter-query" {
				cfg.Payload.Override = nil
				cfg.Payload.Filter = []config.PayloadFilterRule{{Models: []config.PayloadModelRule{rule}, Params: []string{`input.0.content.#(type==input_image)#`}}}
			}
			var firstWire []byte
			for _, enabled := range []bool{false, true} {
				cfg.Kilo.ImageReductionEnabled = enabled
				f.executor.UpdateConfig(cfg)
				before := helps.PayloadConfigEvaluationCount()
				status, body := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", kiloPayloadBody("responses", false, "conditional source", imageURL))
				if status != 200 {
					t.Fatalf("enabled=%t HTTP=%d %s", enabled, status, body)
				}
				wire := <-f.requests
				if helps.PayloadConfigEvaluationCount()-before != 1 {
					t.Fatal("payload rules evaluated more than once")
				}
				if firstWire == nil {
					firstWire = wire
				} else if !bytes.Equal(firstWire, wire) {
					t.Fatal("conditional rule result changed with reduction enabled")
				}
			}
			f.assertReleased(t, 2)
		})
	}
}

func TestKiloPayloadHTTPExcludedCompletions(t *testing.T) {
	f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
	kiloPayloadEnable(t, f, true)
	doc := ojson.NewDocString(`{"model":"gpt-6.1-sol","stream":false}`)
	_ = doc.Set("prompt", strings.Repeat("x", kiloPayloadLimit+100))
	status, body := f.request(t, "POST", "/v1/completions", "synthetic-kilo-key", doc.String())
	if status != 200 {
		t.Fatalf("excluded completions HTTP=%d %s", status, body)
	}
	wire := <-f.requests
	if len(wire) <= kiloPayloadLimit {
		t.Fatal("excluded-route fixture not over limit")
	}
	f.assertReleased(t, 1)
}
