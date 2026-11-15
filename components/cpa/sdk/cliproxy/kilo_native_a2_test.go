package cliproxy

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestKiloA2NativeAlphaBatch(t *testing.T) {
	var machines, sessions []string
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		machines = append(machines, r.Header.Get("X-KILOCODE-MACHINEID"))
		sessions = append(sessions, r.Header.Get("X-Session-ID"))
		raw, _ := io.ReadAll(r.Body)
		if ojson.GetBytes(raw, "tools.0.external_web_access").Bool() || ojson.GetBytes(raw, "max_output_tokens").Int() != 2500 {
			t.Error("cached or short contract changed")
		}
		if ojson.GetBytes(raw, "input.0.content").String() != "complete assistant context" {
			t.Error("assistant input changed")
		}
		kiloA1ClientOutput(w, fmt.Sprintf("resp-a2-%d", len(machines)), []any{map[string]any{"type": "web_search_call", "id": fmt.Sprintf("ws-%d", len(machines)), "status": "completed", "action": map[string]any{"type": "search", "sources": []any{map[string]any{"url": "https://example.com/synthetic", "title": "Synthetic source"}}}}, kiloA1Message("msg-a2", "A2 search result")})
	})
	status, data := f.request(t, "POST", "/v1/alpha/search", "synthetic-kilo-key", `{"id":"main-thread","model":"gpt-6.1-sol","input":[{"role":"assistant","content":"complete assistant context"}],"commands":{"search_query":[{"q":"first"},{"q":"second"}],"response_length":"short"}}`)
	if status != 200 || !strings.Contains(ojson.GetBytes(data, "output").String(), "https://example.com/synthetic") {
		t.Fatalf("alpha=%d %s", status, data)
	}
	f.assertReleased(t, 2)
	if f.posts.Load() != 2 || len(machines) != 2 || machines[0] != machines[1] || sessions[0] == "" || sessions[0] != sessions[1] || sessions[0] == "main-thread" {
		t.Fatalf("POSTs=%d machines=%v sessions=%v", f.posts.Load(), machines, sessions)
	}
	files, err := os.ReadDir(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings"))
	if err != nil || len(files) != 1 {
		t.Fatalf("binding documents=%d err=%v", len(files), err)
	}
	stored, err := os.ReadFile(filepath.Join(config.KiloStateDirectory(f.cfg), "bindings", files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	records := ojson.GetBytes(stored, "bindings").Array()
	if len(records) != 1 || len(records[0].Get("responses").Array()) != 2 || !strings.Contains(records[0].Get("sessions").Raw, "kilo-alpha:main-thread:") {
		t.Fatalf("search source association or batch binding lost: %s", stored)
	}
}

func TestKiloA2NativeAlphaPreflight(t *testing.T) {
	f := newKiloNativeFixture(t, kiloSuccessStream)
	for _, body := range []string{
		`{"id":"synthetic","model":"gpt-6.1-sol","commands":{"search_query":[{"q":"good"},{"q":"bad","recency":7}]}}`,
		`{"id":"synthetic","model":"missing-kilo-model","commands":{"search_query":[{"q":"query"}]}}`,
		`{"id":"synthetic","model":"gpt-6.1-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}],"commands":{"search_query":[{"q":"query"}]}}`,
	} {
		status, data := f.request(t, "POST", "/v1/alpha/search", "synthetic-kilo-key", body)
		if status < 400 {
			t.Fatalf("preflight=%d %s", status, data)
		}
	}
	if f.posts.Load() != 0 {
		t.Fatal("invalid batch went upstream")
	}
}
