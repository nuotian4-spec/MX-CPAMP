package executor

import (
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestBPSDelegationIdentityClassification(t *testing.T) {
	for _, mode := range []string{"notice", "tool_before", "tool_after", "empty_id", "send_message_to_thread", "nested_encrypted", "top_encrypted", "nested_call", "nested_compaction", "tool_role"} {
		t.Run(mode, func(t *testing.T) {
			notice := map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "create_thread", "output": "<codex_delegation>task</codex_delegation>"}
			input := []any{notice}
			switch mode {
			case "tool_before":
				input = []any{map[string]any{"type": "function_call", "call_id": "call_real", "name": "echo", "arguments": "{}"}, notice}
			case "tool_after":
				input = append(input, map[string]any{"type": "function_call_output", "call_id": "call_real", "output": "ok"})
			case "empty_id":
				notice["call_id"] = ""
			case "send_message_to_thread":
				notice["name"] = "send_message_to_thread"
			case "nested_encrypted":
				notice["content"] = []any{map[string]any{"type": "encrypted_content", "encrypted_content": "synthetic"}}
			case "top_encrypted":
				notice["encrypted_content"] = "synthetic"
			case "nested_call":
				notice["content"] = []any{map[string]any{"type": "function_call", "call_id": "call_other"}}
			case "nested_compaction":
				notice["content"] = []any{map[string]any{"type": "compaction", "encrypted_content": "state"}}
			case "tool_role":
				notice["role"] = "tool"
			}
			doc := ojson.NewDocString(`{}`)
			_ = doc.Set("input", input)
			raw := doc.String()
			want := mode != "notice"
			if got := HasBPSIdentityBoundHistory(doc.Bytes()); got != want {
				t.Fatalf("identity-bound=%v want=%v", got, want)
			}
			if !HasAccountBoundHistory(doc.Bytes()) {
				t.Fatal("generic failover boundary changed")
			}
			if doc.String() != raw {
				t.Fatal("caller request mutated")
			}
		})
	}
}
