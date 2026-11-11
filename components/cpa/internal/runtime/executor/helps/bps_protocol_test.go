package helps

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestBPSPrepareTextAndHeaders(t *testing.T) {
	doc := ojson.NewDocString(`{"input":"hello","instructions":"obey","reasoning":{"effort":"high"}}`)
	if err := PrepareBPSDoc(doc, "configured-model"); err != nil {
		t.Fatal(err)
	}
	if doc.Get("model_selection").String() != "explicit" || doc.Get("input.#").Int() != 3 || doc.Get("reasoning_effort").String() != "high" || !doc.Get("stream").Bool() {
		t.Fatalf("unexpected body: %s", doc.String())
	}
	req, _ := http.NewRequest(http.MethodPost, BPSResponsesURL, nil)
	if err := ApplyBPSHeaders(req, "excel-token", "account"); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("chatgpt-account-id") != "account" || req.Header.Get("x-basispoints-auth-mode") != "chatgpt" || req.Header.Get("x-openai-internal-basispoints-client-agent-profile") != "excel" {
		t.Fatal("missing BPS headers")
	}
}

func TestBPSReadEventsTerminalAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		fail                    bool
		count                   int
	}{
		{"complete", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\ndata: {\"type\":\"response.completed\",\n" + "data: \"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}\n\n", "text/event-stream", false, 2},
		{"truncated", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n", "text/event-stream", true, 1},
		{"failed", "data: {\"type\":\"response.failed\"}\n\n", "text/event-stream", true, 0},
		{"tool", "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\"}}\n\n", "text/event-stream", true, 0},
		{"done-only", "data: [DONE]\n\n", "text/event-stream", true, 0},
		{"unary", `{"id":"resp_1","status":"completed","output":[]}`, "application/json", false, 1},
		{"unary-invalid", `{"id":"resp_1","status":"incomplete","output":[]}`, "application/json", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{"Content-Type": []string{tc.contentType}}}
			count := 0
			err := ReadBPSEvents(context.Background(), response, func([]byte) error { count++; return nil })
			if (err != nil) != tc.fail || count != tc.count {
				t.Fatalf("error=%v, events=%d", err, count)
			}
		})
	}
}

func TestBPSReadEventsReindexesVisibleOutput(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","output":[]}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","encrypted_content":"private"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.content_part.added","item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"output_text","text":""}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"hello"}`,
		`{"type":"response.output_text.done","item_id":"msg_1","output_index":1,"content_index":0,"text":"hello"}`,
		`{"type":"response.content_part.done","item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"output_text","text":"hello"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"rs_1","type":"reasoning","encrypted_content":"private"},{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}}`,
	}
	response := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + strings.Join(events, "\n\ndata: ") + "\n\n")), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	visible := 0
	err := ReadBPSEvents(context.Background(), response, func(event []byte) error {
		visible++
		root := ojson.ParseBytes(event)
		if strings.Contains(string(event), "private") || strings.Contains(string(event), "rs_1") {
			t.Fatal("hidden reasoning was published")
		}
		if index := root.Get("output_index"); index.Exists() && index.Int() != 0 {
			t.Fatalf("visible message index=%d, terminal position must be 0: %s", index.Int(), event)
		}
		if root.Get("type").String() == "response.completed" && (root.Get("response.output.#").Int() != 1 || root.Get("response.output.0.id").String() != "msg_1") {
			t.Fatalf("terminal message layout changed: %s", event)
		}
		return nil
	})
	if err != nil || visible != 8 {
		t.Fatalf("error=%v visible=%d", err, visible)
	}
}
