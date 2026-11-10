package basispoints

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestUpstreamSSEFailureUsesPublicWhitelist(t *testing.T) {
	for _, kind := range []string{"response.failed", "response.cancelled", "response.incomplete", "error"} {
		t.Run(kind, func(t *testing.T) {
			_, bridge, err := Prepare([]byte(`{"model":"model","input":"test"}`), "account:1/key:1/thread:one", new(ReplayCache))
			if err != nil {
				t.Fatal(err)
			}
			payload := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_test","status":"failed","output":[{"type":"message","content":[{"text":"synthetic-secret"}]}],"error":{"message":"Authorization: Bearer synthetic-secret"},"usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":2,"secret":"synthetic-secret"},"output_tokens_details":{"reasoning_tokens":1},"detail":"synthetic-secret"},"detail":"synthetic-secret"},"message":"synthetic-secret","detail":"synthetic-secret"}`, kind)
			body := bridge.Stream(io.NopCloser(strings.NewReader("event: " + kind + "\ndata: " + payload + "\n\n")))
			defer body.Close()
			output, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(output), "synthetic-secret") {
				t.Fatalf("失败终态泄露上游诊断：%s", output)
			}
			if strings.Count(string(output), "event:") != 1 {
				t.Fatalf("重复终态：%s", output)
			}
			if !strings.Contains(string(output), `"input_tokens":12`) || !strings.Contains(string(output), `"cached_tokens":2`) {
				t.Fatalf("丢失安全用量：%s", output)
			}
			if !strings.Contains(string(output), `"id":"resp_test"`) {
				t.Fatalf("丢失响应标识：%s", output)
			}
		})
	}
}

func TestFailureDoesNotCopyNonNumericUsage(t *testing.T) {
	_, bridge, err := Prepare([]byte(`{"model":"model","input":"test"}`), "scope", new(ReplayCache))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(object{"type": "response.failed", "response": object{"usage": object{"input_tokens": "private-value", "output_tokens": object{"echo": "private-value"}}}})
	body := bridge.Stream(io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\n")))
	defer body.Close()
	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "private-value") {
		t.Fatalf("非数字用量进入客户端：%s", out)
	}
}
