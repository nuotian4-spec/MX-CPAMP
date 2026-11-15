package helps

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBPSFailoverFactsAndUsage(t *testing.T) {
	wire := `{"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":7},"error":{"code":"basispoints_protocol_error","failure_stage":"bridge_tool_validation","upstream_terminal_event":"response.completed","tool_batch_withheld":true}}}`
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + wire + "\n\n"))}
	err := readBPSEvents(context.Background(), response, true, func([]byte) error { t.Fatal("failed batch escaped"); return nil })
	f, ok := WithBPSHTTPStatus(err, 200).(*BPSFailure)
	if !ok {
		t.Fatal(err)
	}
	complete, withheld := f.BPSFailoverFacts()
	if !complete || !withheld {
		t.Fatal("typed facts lost")
	}
	detail, ok := BPSFailureUsage(f)
	if !ok || detail.InputTokens != 7 || detail.Iterations == nil {
		t.Fatal("usage lost")
	}
	if (*detail.Iterations)[0].OutputTokens != nil {
		t.Fatal("unknown output usage became zero")
	}
}
