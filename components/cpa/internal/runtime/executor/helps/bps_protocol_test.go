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
