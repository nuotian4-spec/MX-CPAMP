package requestlatency

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRequestLatencyStateContract(t *testing.T) {
	ctx, state := NewContext(context.Background())
	now := time.Unix(1, 0)
	state.entry = now
	state.now = func() time.Time { return now }
	ctx = WithRoute(ctx, DirectPlugin, false, false, 31)
	ctx = WithRoute(ctx, CredentialExecutor, true, true, 999)
	state.Executor("cpa-bps", false)
	state.Executor("secret-plugin-token", false)
	end := state.Measure(SelectionLockWait)
	now = now.Add(7 * time.Millisecond)
	end()
	ctx = StartPlugin(ctx)
	PluginOutput(ctx, []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"private"))
	now = now.Add(9 * time.Millisecond)
	PluginOutput(ctx, []byte("-answer\"}\n\n"))
	state.Failure(errors.New("secret-raw-error"))
	fields := state.Finish(nil, 200)
	for key, want := range map[string]any{
		"route": "direct_plugin", "admission_mode": "home", "execution_input_bytes": 31,
		"selection_lock_wait_ms": float64(7), "selection_lock_wait_count": 1,
		"nested_plugin_first_output_ms": float64(9), "nested_plugin_first_output_count": 1,
		"nested_credential_executor_count": 1, "executor_cpa_bps_count": 1,
		"executor_other_plugin_count": 1, "outcome": "failed",
	} {
		if fields[key] != want {
			t.Fatalf("%s=%v want %v", key, fields[key], want)
		}
	}
	for _, absent := range []string{"credential_wait_ms", "plugin_first_output_ms", "bps_upload_ms", "host_first_output_ms"} {
		if _, ok := fields[absent]; ok {
			t.Fatalf("unobserved field present: %s", absent)
		}
	}
	if state.Finish(nil, 200) != nil {
		t.Fatal("duplicate completion")
	}
	for _, secret := range []string{"private-answer", "secret-raw-error", "secret-plugin-token"} {
		if strings.Contains(fmt.Sprint(fields), secret) {
			t.Fatal("sensitive value escaped")
		}
	}
	_, early := NewContext(context.Background())
	if _, ok := early.Finish(context.Canceled, 200)["execution_input_bytes"]; ok {
		t.Fatal("unknown input represented as zero")
	}
}

func TestRequestLatencyOutputAcrossChunks(t *testing.T) {
	for _, tc := range []struct{ name, input, kind string }{
		{"text", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n", "text"},
		{"tool", "data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{}\"}\r\n\r\n", "tool"},
		{"heartbeat", ": ping\n\n", ""},
		{"terminal", "data: {\"type\":\"response.completed\"}\n\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			calls := 0
			output := NewOutput(func(kind string) { got = kind; calls++ })
			for _, b := range []byte(tc.input) {
				output.Write([]byte{b})
			}
			if got != tc.kind {
				t.Fatalf("kind=%q want %q", got, tc.kind)
			}
			output.Write([]byte(tc.input))
			if calls > 1 {
				t.Fatal("duplicate first output")
			}
		})
	}
	output := NewOutput(func(string) { t.Fatal("oversize input observed") })
	output.Write(make([]byte, (1<<20)+1))
	if !output.done || output.pending != nil {
		t.Fatal("observer not bounded")
	}
}
