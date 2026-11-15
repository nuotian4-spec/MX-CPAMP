package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type kiloPayloadGatedContext struct {
	context.Context
	checks  atomic.Int32
	entered chan<- struct{}
}

func (c *kiloPayloadGatedContext) Err() error {
	// The helper's second cancellation check is inside an acquired image slot.
	if c.checks.Add(1) == 2 {
		c.entered <- struct{}{}
		<-c.Done()
	}
	return c.Context.Err()
}

type kiloPayloadGatedExecutor struct {
	*native.KiloExecutor
	started atomic.Int32
	entered chan<- struct{}
	stopped chan<- struct{}
}

func (e *kiloPayloadGatedExecutor) PrepareKiloPayload(ctx context.Context, req core.Request, opts core.Options) (context.Context, error) {
	if e.started.Add(1) <= 2 {
		defer func() { e.stopped <- struct{}{} }()
		return e.KiloExecutor.PrepareKiloPayload(&kiloPayloadGatedContext{Context: ctx, entered: e.entered}, req, opts)
	}
	return e.KiloExecutor.PrepareKiloPayload(ctx, req, opts)
}

func TestKiloPayloadHTTPBusyAndCancellationBeforeAdmission(t *testing.T) {
	f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
	kiloPayloadEnable(t, f, true)
	body := kiloPayloadBody("responses", true, "cancel-before-admission", kiloPayloadLargeImage(t))
	entered, stopped := make(chan struct{}, 2), make(chan struct{}, 2)
	f.manager.RegisterExecutor(&kiloPayloadGatedExecutor{KiloExecutor: f.executor, entered: entered, stopped: stopped})
	before := kiloPayloadBindingFiles(t, f)
	var cancels []context.CancelFunc
	clients := make(chan error, 2)
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		defer cancel()
		go func() {
			req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
			req.Header.Set("Content-Type", "application/json")
			resp, err := f.server.Client().Do(req)
			if resp != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			clients <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("preparation slot gate not reached")
		}
	}
	f.assertReleased(t, 0)
	status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", body)
	if status != 503 || !bytes.Contains(data, []byte("kilo_image_preparation_busy")) {
		t.Fatalf("busy=%d %s", status, data)
	}
	for _, cancel := range cancels {
		cancel()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatal("preparation cancellation did not release slot")
		}
		if err := <-clients; err == nil {
			t.Fatal("canceled HTTP client unexpectedly succeeded")
		}
	}
	f.assertReleased(t, 0)
	if f.posts.Load() != 0 || f.connects.Load() != 0 || !maps.Equal(before, kiloPayloadBindingFiles(t, f)) {
		t.Fatal("pre-admission busy/cancel changed upstream or bindings")
	}
	status, data = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", body)
	if status != 200 || !bytes.Contains(data, []byte("response.completed")) {
		t.Fatalf("slots not reusable: %d %s", status, data)
	}
	f.assertReleased(t, 1)
	t.Log("two real preparation slots occupied; busy=503; cancel/busy POST=0 results=0 bindings unchanged; next request success")
}

func TestKiloPayloadHTTPStricterGlobalLimit(t *testing.T) {
	f := newKiloNativeFixture(t, kiloPayloadSuccessStream)
	cfg := f.cfg.CloneForRuntime()
	cfg.MaxRequestBodyBytes = 1024
	f.server.Close()
	server := api.NewServer(cfg, f.manager, access.NewManager(), f.configPath)
	f.server = httptest.NewServer(server.Handler())
	f.server.Client().Transport = &http.Transport{Proxy: nil}
	t.Cleanup(f.server.Close)
	status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", kiloPayloadBody("responses", false, "short", ""))
	if status != 200 {
		t.Fatalf("short=%d %s", status, data)
	}
	status, _ = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", kiloPayloadBody("responses", false, strings.Repeat("x", 2048), ""))
	if status != 413 || f.posts.Load() != 1 {
		t.Fatalf("stricter limit=%d POST=%d", status, f.posts.Load())
	}
	f.assertReleased(t, 1)
}

func TestKiloPayloadHTTPLargeContinuationFidelity(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, seen := kiloM7BFixture(t, "function_call", "call-payload-preserve")
			owner := kiloM7BStart(t, f, seen, "call-payload-preserve")
			makeBody := func(url string) string {
				doc := ojson.NewDocString(`{"model":"gpt-6.1-sol","input":[{"role":"system","content":"HEAD-system"},{"role":"developer","content":"developer-exact"},{"type":"compaction","encrypted_content":"compaction-cipher-exact"},{"type":"reasoning","encrypted_content":"reasoning-cipher-exact"},{"type":"function_call","id":"fc-preserve","call_id":"call-payload-preserve","name":"source_tool","arguments":"参数 exact"},{"type":"function_call_output","call_id":"call-payload-preserve","output":[{"type":"input_text","text":"MIDDLE-tool-exact"},{"type":"input_image","image_url":"","detail":"high"}]},{"role":"user","content":[{"type":"input_text","text":"TAIL-user-exact"},{"type":"input_image","file_id":"file-preserve","detail":"low"}]}]}`)
				_ = doc.Set("input.5.output.1.image_url", url)
				_ = doc.Set("stream", stream)
				return doc.String()
			}
			kiloPayloadEnable(t, f, true)
			status, data := f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", makeBody(kiloPayloadLargeImage(t)))
			if status != 200 {
				t.Fatalf("large continuation=%d %s", status, data)
			}
			wire := <-f.requests
			reduced := ojson.GetBytes(wire, "input.5.output.1.image_url").String()
			if reduced == "" || reduced == kiloPayloadLargeImage(t) {
				t.Fatal("tool image was not reduced")
			}
			kiloM7BAssertRound(t, f, seen, owner, 2)
			kiloPayloadEnable(t, f, false)
			status, data = f.request(t, "POST", "/v1/responses", "synthetic-kilo-key", makeBody(reduced))
			if status != 200 {
				t.Fatalf("equivalent continuation=%d %s", status, data)
			}
			control := <-f.requests
			if !reflect.DeepEqual(ojson.ParseBytes(wire).Value(), ojson.ParseBytes(control).Value()) {
				t.Fatal("non-image continuation fields differ from disabled-reduction control")
			}
			for _, marker := range []string{"HEAD-system", "developer-exact", "compaction-cipher-exact", "reasoning-cipher-exact", "call-payload-preserve", "MIDDLE-tool-exact", "TAIL-user-exact", "file-preserve"} {
				if !bytes.Contains(wire, []byte(marker)) {
					t.Fatalf("fidelity marker missing: %s", marker)
				}
			}
			kiloM7BAssertRound(t, f, seen, owner, 3)
			t.Logf("large tool continuation final=%d bytes; disabled equivalent deep-equal; same account/session", len(wire))
		})
	}
}
