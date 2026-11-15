package helps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloM7PureFunctionComparison(t *testing.T) {
	terminal := `data: {"type":"response.completed","response":{"id":"r","status":"completed","model":"openai/gpt-6.1-sol","output":[],"unknown":{"preserved":true}}}` + "\n\n"
	cases := []struct{ name, content, body string }{
		{"event-error", "text/event-stream", "event: error\ndata: {\"message\":\"SYNTHETIC_PRIVATE\"}\n\n" + terminal},
		{"top-error", "text/event-stream", "data: {\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\",\"type\":\"usage_limit_error\",\"message\":\"SYNTHETIC_PRIVATE\"},\"sequence_number\":7}\n\n" + terminal},
		{"code-message", "text/event-stream", "data: {\"code\":\"synthetic_unknown_error\",\"message\":\"SYNTHETIC_PRIVATE\"}\n\n" + terminal},
		{"control-model-unknown-tail", "text/event-stream", ": keepalive\n\nid: native-id\nretry: 77\n\nevent: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"model\":\"openai/gpt-6.1-sol\",\"unknown\":1}}\n\nevent: custom\ndata: {\"type\":\"custom\",\"keep\":true}\n\n" + terminal + "data: [DONE]\n\n"},
		{"json-model", "application/json", `{"id":"r","status":"completed","model":"openai/gpt-6.1-sol","output":[],"unknown":1}`},
	}
	rows := make([]any, 0, len(cases))
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{sample.content}}, Body: io.NopCloser(strings.NewReader(sample.body))}
			var frames bytes.Buffer
			err := ReadKiloEvents(context.Background(), response, func(frame, event []byte) error {
				frame, _ = KiloPublicEvent(frame, event)
				frames.Write(frame)
				return nil
			})
			row := map[string]any{"name": sample.name, "content_type": sample.content, "input": sample.body, "cpa_frames": frames.String(), "cpa_error": "", "cpa_public_error": ""}
			if err != nil {
				row["cpa_error"] = err.Error()
				var typed *KiloResponseError
				if errors.As(err, &typed) {
					row["cpa_public_error"] = typed.PublicErrorBody()
					row["cpa_status"] = typed.StatusCode()
				}
			}
			if strings.Contains(sample.name, "error") || sample.name == "code-message" {
				if err == nil || frames.Len() != 0 {
					t.Fatal("error frame or later completed escaped")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(frames.String(), "openai/") {
				t.Fatal("public model prefix survived")
			}
			rows = append(rows, row)
		})
	}
	if path := os.Getenv("KILO_M7_COMPARISON_OUTPUT"); path != "" {
		doc := ojson.NewDocString("{}")
		_ = doc.Set("cases", rows)
		if err := os.WriteFile(path, doc.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type kiloNoTailReader struct {
	first []byte
	reads int
}

func (r *kiloNoTailReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.first) == 0 {
		return 0, errors.New("unexpected read after terminal")
	}
	n := copy(p, r.first)
	r.first = r.first[n:]
	return n, nil
}
func TestKiloM7TerminalDoesNotWaitForTail(t *testing.T) {
	r := &kiloNoTailReader{first: []byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(r)}
	if err := ReadKiloEvents(context.Background(), response, func([]byte, []byte) error { return nil }); err != nil || r.reads != 1 {
		t.Fatalf("terminal read=%d err=%v", r.reads, err)
	}
}
func TestKiloM7ModelOnlyOnePrefix(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol", "openai/openai/gpt-6.1-sol"} {
		doc := ojson.NewDocString(`{"type":"response.created","unknown":true,"response":{"model":""}}`)
		_ = doc.Set("response.model", model)
		frame := append(append([]byte("id: keep\nevent: response.created\ndata: "), doc.Bytes()...), []byte("\nretry: 77\n\n")...)
		changed, event := KiloPublicEvent(frame, doc.Bytes())
		if ojson.GetBytes(event, "response.model").String() != strings.TrimPrefix(model, "openai/") || !ojson.GetBytes(event, "unknown").Bool() || !bytes.HasPrefix(changed, []byte("id: keep\nevent: response.created\n")) || !bytes.HasSuffix(changed, []byte("\nretry: 77\n\n")) {
			t.Fatalf("model-only normalization=%s", changed)
		}
	}
}
