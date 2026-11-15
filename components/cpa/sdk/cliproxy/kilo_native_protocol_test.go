package cliproxy

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestKiloNativeChatToolImageTranslation(t *testing.T) {
	var calls atomic.Int32
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		doc := ojson.NewDocString(kiloTerminal)
		_ = doc.Set("response.id", fmt.Sprintf("protocol-round-%d", n))
		if n == 1 {
			_ = doc.Set("response.output", []any{map[string]any{"type": "function_call", "id": "fc-chat", "call_id": "call-chat", "name": "source_tool", "arguments": "{\"exact\":1}", "status": "completed"}})
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", doc.String())
	})
	status, data := f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"request source tool"}],"tools":[{"type":"function","function":{"name":"source_tool","parameters":{"type":"object"}}}]}`)
	if status != 200 {
		t.Fatalf("initial tool call=%d %s", status, data)
	}
	f.assertReleased(t, 1)
	<-f.requests
	source := `{"model":"gpt-6.1-sol","messages":[{"role":"user","content":[{"type":"text","text":"synthetic"},{"type":"image_url","image_url":{"url":"data:image/png;base64,c3ludGhldGlj"}}]},{"role":"assistant","tool_calls":[{"id":"call-chat","type":"function","function":{"name":"source_tool","arguments":"{\"exact\":1}"}}]},{"role":"tool","tool_call_id":"call-chat","content":"exact-result"}],"tools":[{"type":"function","function":{"name":"source_tool","parameters":{"type":"object"}}}]}`
	status, data = f.request(t, "POST", "/v1/chat/completions", "synthetic-kilo-key", source)
	if status != 200 {
		t.Fatalf("chat=%d %s", status, data)
	}
	wire := <-f.requests
	foundImage, foundCall, foundOutput := false, false, false
	for _, item := range ojson.GetBytes(wire, "input").Array() {
		switch item.Get("type").String() {
		case "function_call":
			foundCall = item.Get("call_id").String() == "call-chat" && item.Get("arguments").String() == `{"exact":1}`
		case "function_call_output":
			foundOutput = item.Get("call_id").String() == "call-chat" && item.Get("output").String() == "exact-result"
		}
		for _, part := range item.Get("content").Array() {
			if part.Get("type").String() == "input_image" {
				foundImage = part.Get("image_url").String() == "data:image/png;base64,c3ludGhldGlj"
			}
		}
	}
	if !foundImage || !foundCall || !foundOutput {
		t.Fatalf("chat tools/images lost: %s", wire)
	}
	f.assertReleased(t, 2)
	if calls.Load() != 2 || f.posts.Load() != 2 {
		t.Fatal("protocol rounds must each send exactly once")
	}
}

func TestKiloNativeAuthFileAndStaticModelLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{AuthDir: dir}
	auths, err := synthesizer.SynthesizeAuthFile(&synthesizer.SynthesisContext{Config: cfg, AuthDir: dir, Now: time.Now()}, filepath.Join(dir, "synthetic-kilo.json"), []byte(`{"type":"kilo","api_key":"synthetic-only","organization_id":"synthetic-org","allow_direct_when_proxy_full":true,"priority":3}`))
	if err != nil || len(auths) != 1 {
		t.Fatalf("auth synthesis=%v %v", auths, err)
	}
	a := auths[0]
	if a.Provider != "kilo" || a.Metadata["organization_id"] != "synthetic-org" || a.Metadata["allow_direct_when_proxy_full"] != true || a.Attributes["priority"] != "3" {
		t.Fatalf("auth metadata=%+v", a)
	}
	manager := coreauth.NewManager(nil, nil, nil)
	a, err = manager.Register(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: cfg, coreManager: manager}
	service.registerExecutorForAuth(a, false)
	first, _ := manager.Executor("kilo")
	if !service.registerModelsForAuth(context.Background(), a) {
		t.Fatal("auth publication rejected")
	}
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(a.ID) })
	models := registry.GetGlobalRegistry().GetAvailableModelsByProvider("kilo")
	for _, name := range helps.KiloModels {
		found := false
		for _, m := range models {
			if m.ID == name {
				found = true
			}
		}
		if !found {
			t.Errorf("missing static model %s", name)
		}
	}
	service.registerExecutorForAuth(a, false)
	second, _ := manager.Executor("kilo")
	if first != second {
		t.Fatal("executor runtime recreated per credential")
	}
	if strings.Contains(a.ID, "synthetic-only") {
		t.Fatal("credential secret leaked into identity")
	}
}
