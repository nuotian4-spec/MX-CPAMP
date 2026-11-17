package kilolatency

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKiloLatencyTimelineAndMissing(t *testing.T) {
	ctx, state := NewContext(context.Background())
	now := time.Unix(100, 0)
	state.entry, state.now = now, func() time.Time { return now }
	state.Enable(true)
	wait := state.Measure("capacity_wait")
	now = now.Add(2 * time.Millisecond)
	wait()
	state.Mark("admitted")
	now = now.Add(time.Millisecond)
	state.Mark("sent")
	now = now.Add(time.Millisecond)
	state.Mark("headers")
	now = now.Add(500 * time.Microsecond)
	state.Mark("text")
	now = now.Add(time.Millisecond)
	state.Mark("flushed")
	fields := state.Finish(nil, 200)
	for name, want := range map[string]any{"outcome": "success_text", "mode": "stream", "capacity_wait_ms": float64(2), "admission_elapsed_ms": float64(2), "upstream_headers_ms": float64(1), "upstream_first_text_ms": float64(1.5), "cpa_first_text_ms": float64(5.5), "first_text_forward_ms": float64(1), "server_total_ms": float64(5.5)} {
		if fields[name] != want {
			t.Fatalf("%s=%v want %v", name, fields[name], want)
		}
	}
	if _, exists := fields["session_wait_ms"]; exists {
		t.Fatal("missing session hook became a zero sample")
	}
	if state.Finish(context.Canceled, 200) != nil {
		t.Fatal("duplicate summary")
	}
	if From(Inherit(context.Background(), ctx)) != state {
		t.Fatal("state was not inherited")
	}
	_, missing := NewContext(context.Background())
	missing.Enable(true)
	if _, ok := missing.TTFT(); ok {
		t.Fatal("unknown TTFT became observed")
	}
	if _, ok := missing.Finish(nil, 200)["upstream_first_text_ms"]; ok {
		t.Fatal("unknown TTFT was serialized")
	}
}

func TestKiloLatencyOutcomesAndFrozenUsage(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		status     int
		tool, text bool
		want       string
	}{
		{"tool", nil, 200, true, false, "success_tool"}, {"failure", errors.New("private failure"), 200, false, false, "failed"},
		{"cancel", context.Canceled, 200, false, false, "canceled"}, {"after_text", context.Canceled, 200, false, true, "canceled"},
		{"reject", nil, 429, false, false, "rejected"}, {"normal", nil, 200, false, true, "success_text"},
		{"storage_failure", nil, 503, false, false, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, s := NewContext(context.Background())
			s.Enable(true)
			s.Mark("sent")
			frozen := WithUsageTTFT(ctx, 0, false)
			if test.text {
				s.Mark("text")
			}
			if test.tool {
				s.Tool()
			}
			s.Failure(test.err)
			if _, ok := UsageTTFT(frozen); ok {
				t.Fatal("early publication mutated after text")
			}
			if got := s.Finish(nil, test.status)["outcome"]; got != test.want {
				t.Fatalf("outcome=%v", got)
			}
		})
	}
	ctx := WithUsageTTFT(context.Background(), 500*time.Microsecond, true)
	if duration, ok := UsageTTFT(ctx); !ok || duration != 500*time.Microsecond {
		t.Fatal("observed sub-ms sample lost")
	}
}

func TestKiloLatencyContentAndWrittenFrames(t *testing.T) {
	for _, event := range []string{
		`{"type":"response.created"}`, `{"type":"response.reasoning_summary_text.delta","delta":"reason"}`,
		`{"type":"response.function_call_arguments.delta","delta":"args"}`, `{"type":"response.output_text.done","text":"answer"}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"terminal answer"}]}]}}`,
	} {
		if text, _ := Content([]byte(event)); text {
			t.Fatalf("non-body classified as text: %s", event)
		}
	}
	for _, event := range []string{`{"type":"response.output_text.delta","delta":"answer"}`, `{"choices":[{"delta":{"content":"answer"}}]}`, `{"type":"response.output_item.added","item":{"type":"message","content":[{"type":"output_text","text":"answer"}]}}`} {
		out := Output{}
		out.Write([]byte(": heartbeat\n\n"))
		out.Write([]byte("data: " + event[:10]))
		if out.Text {
			t.Fatal("incomplete frame classified as text")
		}
		out.Write([]byte(event[10:] + "\n\n"))
		if !out.Text {
			t.Fatal("written text not observed")
		}
	}
}
