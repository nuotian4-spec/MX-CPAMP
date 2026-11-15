package executor

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/bpsbinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestBPSDelegationCollaborationFixture(t *testing.T) {
	cfg := &config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}}
	request := ojson.NewDocString(`{"model":"configured-model","tools":[{"type":"namespace","name":"collaboration-optimize","tools":[{"type":"function","name":"followup_task","parameters":{"type":"object","properties":{"target":{"type":"string"},"message":{"type":"string"}},"required":["target","message"]}}]}]}`)
	history := []any{map[string]any{"role": "user", "content": "PARENT_MARKER continue after a failed subagent operation"}, delegationNotice()}
	_ = request.Set("input", history)
	args := ojson.NewDocString(`{"summary":"Call client tool","extended_summary":"","destructive":false,"references":[]}`)
	_ = args.Set("code", `{"name":"collaboration-optimize.followup_task","arguments":{"target":"synthetic-missing-agent","message":"CHILD_MARKER"}}`)
	terminal := ojson.NewDocString(bpsTestTerminal)
	_ = terminal.Set("response.output", []any{map[string]any{"id": "fc_followup", "type": "function_call", "call_id": "call_native_followup", "name": "run_officejs", "arguments": args.String(), "status": "completed"}})
	fixture := ojson.NewDocString(`{}`)
	posts := 0
	transport := func(r *http.Request) (*http.Response, error) {
		posts++
		body, _ := io.ReadAll(r.Body)
		_ = fixture.Set("upstream_request_"+string(rune('0'+posts)), ojson.ParseBytes(body).Value())
		result := terminal.String()
		if posts == 2 {
			result = bpsTestTerminal
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + result + "\n\n"))}, nil
	}
	first, err := runDelegation(t, cfg, request.Bytes(), "caller", true, transport)
	if err != nil {
		t.Fatal(err)
	}
	_ = fixture.Set("client_sse", string(first))
	var call any
	var id string
	for _, line := range strings.Split(string(first), "\n") {
		event := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		if event.Get("type").String() == "response.completed" {
			item := event.Get("response.output.0")
			call = item.Value()
			id = item.Get("call_id").String()
		}
	}
	if !bpsbinding.IsCallID(id) || call == nil {
		t.Fatalf("public call missing: %s", first)
	}
	failure := map[string]any{"type": "function_call_output", "call_id": id, "output": "collab tool failed: synthetic-missing-agent does not exist ERROR_MARKER"}
	history = append(history, call, failure, map[string]any{"role": "user", "content": "RESUME_MARKER continue without retrying the failed operation"})
	_ = request.Set("input", history)
	_ = fixture.Set("client_continuation", ojson.ParseBytes(request.Bytes()).Value())
	second, err := runDelegation(t, cfg, request.Bytes(), "caller", true, transport)
	if err != nil {
		t.Fatal(err)
	}
	_ = fixture.Set("continuation_sse", string(second))
	_ = fixture.Set("public_call_id", id)
	_ = fixture.Set("native_call_id", "call_native_followup")
	wire := fixture.Get("upstream_request_2").Raw
	if posts != 2 || strings.Contains(wire, id) || !strings.Contains(wire, "call_native_followup") || !strings.Contains(wire, "ERROR_MARKER") || !strings.Contains(wire, "RESUME_MARKER") {
		t.Fatalf("continuation changed: posts=%d wire=%s", posts, wire)
	}
	t.Logf("BPS_FIXTURE=%s", fixture.String())
}
