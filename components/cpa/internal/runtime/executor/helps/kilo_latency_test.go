package helps

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type kiloLatencyUsageCapture func(context.Context, usage.Record)

func (fn kiloLatencyUsageCapture) HandleUsage(ctx context.Context, r usage.Record) { fn(ctx, r) }

func TestKiloLatencyUsagePublicationSnapshot(t *testing.T) {
	type captured struct {
		record usage.Record
		known  bool
	}
	items := make(chan captured, 8)
	name := t.Name()
	usage.RegisterNamedPlugin(name, kiloLatencyUsageCapture(func(ctx context.Context, r usage.Record) {
		if r.Model == name {
			_, known := kilolatency.UsageTTFT(ctx)
			items <- captured{r, known}
		}
	}))
	t.Cleanup(func() {
		usage.RegisterNamedPlugin(name, kiloLatencyUsageCapture(func(context.Context, usage.Record) {}))
	})
	ctx, state := kilolatency.NewContext(context.Background())
	state.Enable(true)
	state.Mark("sent")
	reporter := NewUsageReporter(ctx, "kilo", name, nil)
	tier, ok := ParseCodexUsage([]byte(`{"type":"response.created","response":{"service_tier":"default"}}`))
	if !ok {
		t.Fatal("service-tier-only publication fixture changed")
	}
	reporter.Publish(ctx, tier)
	ObserveKiloLatency(ctx, []byte(`{"type":"response.output_text.delta","delta":"answer"}`))
	reporter.Publish(ctx, usage.Detail{OutputTokens: 1})
	usage.PublishRecord(context.Background(), usage.Record{Provider: "latency-test-barrier", Model: name})
	first := func() captured {
		select {
		case v := <-items:
			return v
		case <-time.After(3 * time.Second):
			t.Fatal("usage dispatch did not finish")
			return captured{}
		}
	}
	if got := first(); got.record.Provider != "kilo" || got.known || got.record.TTFT != 0 {
		t.Fatalf("early usage=%+v", got)
	}
	if got := first(); got.record.Provider != "latency-test-barrier" {
		t.Fatalf("usage was republished: %+v", got)
	}
	if fields := state.Finish(nil, 200); fields["outcome"] != "success_text" {
		t.Fatalf("final observation lost text: %v", fields)
	}
	ctx, state = kilolatency.NewContext(context.Background())
	state.Enable(true)
	state.Mark("sent")
	ObserveKiloLatency(ctx, []byte(`{"type":"response.output_text.delta","delta":"answer"}`))
	NewUsageReporter(ctx, "kilo", name, nil).Publish(ctx, usage.Detail{OutputTokens: 1})
	if got := first(); !got.known || got.record.TTFT < 0 {
		t.Fatalf("known usage=%+v", got)
	}
}
