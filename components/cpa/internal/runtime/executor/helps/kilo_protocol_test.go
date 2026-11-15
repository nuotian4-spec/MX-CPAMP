package helps

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

type kiloFragmentedReader struct{ io.Reader }

func (r kiloFragmentedReader) Read(p []byte) (int, error) {
	if len(p) > 7 {
		p = p[:7]
	}
	return r.Reader.Read(p)
}

func TestKiloProtocolGzipFragmentsAndUnknownSSEFields(t *testing.T) {
	source := "event: synthetic.unknown\nid: source-event\nretry: 77\ndata: {\"type\":\"synthetic.unknown\",\n" + "data: \"custom\":{\"keep\":true}}\n\n"
	terminal := `data: {"type":"response.completed","response":{"status":"completed","output":[],"unknown":"keep"}}` + "\n\n"
	source += terminal
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write([]byte(source))
	_ = gz.Close()
	response := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}, "Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(kiloFragmentedReader{bytes.NewReader(compressed.Bytes())})}
	var frames bytes.Buffer
	var events int
	err := ReadKiloEvents(context.Background(), response, func(frame, event []byte) error {
		events++
		frames.Write(frame)
		if !ojson.ValidBytes(event) {
			t.Fatalf("invalid multiline JSON: %s", event)
		}
		return nil
	})
	if err != nil || events != 2 || frames.String() != source {
		t.Fatalf("SSE fidelity events=%d err=%v frames=%s", events, err, frames.String())
	}
}

func TestKiloProtocolTerminalsAndSizeBoundary(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		wantError               bool
	}{
		{"json-completed", "application/json", `{"id":"r","status":"completed","output":[],"unknown":1}`, false},
		{"json-incomplete", "application/json", `{"status":"incomplete"}`, true},
		{"missing-terminal", "text/event-stream", `data: {"type":"response.created"}` + "\n\n", true},
		{"done-only", "text/event-stream", "data: [DONE]\n\n", true},
		{"failed", "text/event-stream", `data: {"type":"response.failed","response":{"status":"failed"}}` + "\n\n", true},
		{"false-completed", "text/event-stream", `data: {"type":"response.completed","response":{"status":"incomplete"}}` + "\n\n", true},
		{"invalid-json", "text/event-stream", "data: not-json\n\n", true},
		{"oversized-event", "text/event-stream", "data: " + strings.Repeat("x", KiloMaxEventBytes) + "\n\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{Header: http.Header{"Content-Type": []string{test.contentType}}, Body: io.NopCloser(strings.NewReader(test.body))}
			err := ReadKiloEvents(context.Background(), response, func([]byte, []byte) error { return nil })
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v wantError=%t", err, test.wantError)
			}
		})
	}
	prefix := `data: {"type":"response.completed","response":{"status":"completed","padding":"`
	suffix := `"}}` + "\n\n"
	exact := prefix + strings.Repeat("x", KiloMaxEventBytes-len(prefix)-len(suffix)) + suffix
	response := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(exact))}
	if err := ReadKiloEvents(context.Background(), response, func([]byte, []byte) error { return nil }); err != nil {
		t.Fatalf("4MiB boundary rejected: %v", err)
	}
}

func TestKiloAgentMessagePreservesCiphertextAndOrdinaryInput(t *testing.T) {
	raw := `{"input":[{"type":"function_call","call_id":"keep-id","name":"keep-name","arguments":"{\"x\":1}"},{"type":"agent_message","author":"worker","recipient":"parent","routing_extra":"keep","content":[{"type":"input_text","text":"hello"},{"type":"encrypted_content","encrypted_content":"CIPHERTEXT+%/="}]}],"unknown":{"keep":true}}`
	doc := ojson.NewDocString(raw)
	if err := AdaptKiloAgentMessages(doc); err != nil {
		t.Fatal(err)
	}
	output := doc.Get("input").Array()
	if len(output) != 4 {
		t.Fatalf("adapted input=%s", doc.String())
	}
	if !reflect.DeepEqual(output[0].Value(), ojson.Get(raw, "input.0").Value()) {
		t.Fatal("ordinary function call changed")
	}
	callID := output[1].Get("call_id").String()
	if !strings.HasPrefix(callID, "call_kilo_agent_") || output[2].Get("call_id").String() != callID || output[1].Get("namespace").String() != "collaboration" {
		t.Fatalf("transport pairing=%s", doc.String())
	}
	args := ojson.Get(output[1].Get("arguments").String(), "message").String()
	if args != "CIPHERTEXT+%/=" {
		t.Fatalf("ciphertext=%s", args)
	}
	if doc.Get("unknown.keep").Bool() != true || !strings.Contains(output[3].Raw, "routing_extra") {
		t.Fatal("unknown fields discarded")
	}
	repeated := ojson.NewDocString(raw)
	_ = AdaptKiloAgentMessages(repeated)
	if repeated.Get("input.1.call_id").String() != callID {
		t.Fatal("same agent content changed call identity")
	}
	changed := ojson.NewDocString(strings.ReplaceAll(raw, "CIPHERTEXT", "DIFFERENT"))
	_ = AdaptKiloAgentMessages(changed)
	if changed.Get("input.1.call_id").String() == callID {
		t.Fatal("different agent content collided")
	}
	invalid := ojson.NewDocString(`{"input":[{"type":"agent_message","author":"a","recipient":"b","content":[{"type":"unknown"}]}]}`)
	if AdaptKiloAgentMessages(invalid) == nil {
		t.Fatal("unsupported agent content silently discarded")
	}
}
