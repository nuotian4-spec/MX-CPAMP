package executor

import "testing"

func TestBPSUnsupportedReason(t *testing.T) {
	for _, payload := range []string{`{"input":"hello"}`, `{"messages":[{"role":"user","content":"hello"}]}`, `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`} {
		if reason := BPSUnsupportedReason([]byte(payload)); reason != "" {
			t.Fatalf("text request rejected: %s", reason)
		}
	}
	for _, payload := range []string{`{"input":"x","previous_response_id":"resp_1"}`, `{"input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, `{"input":[{"type":"compaction","encrypted_content":"opaque"}]}`, `{"input":[{"type":"function_call_output","call_id":"x"}]}`, `{"messages":[{"role":"tool","content":"x"}]}`, `{"input":"x","tools":[{"type":"function","name":"foo"}]}`, `{"input":[{"type":"message","content":[{"type":"input_image","image_url":"x"}]}]}`, `{"input":"x","max_output_tokens":100}`} {
		if reason := BPSUnsupportedReason([]byte(payload)); reason == "" {
			t.Fatalf("unsupported request accepted: %s", payload)
		}
	}
}
