package redisqueue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func TestKiloLatencyQueueMissingAndSubmillisecond(t *testing.T) {
	for _, test := range []struct {
		name, provider string
		observed       bool
		duration       time.Duration
		present        bool
		want           float64
	}{
		{"missing", "kilo", false, 0, false, 0}, {"subms", "kilo", true, 500 * time.Microsecond, true, 0},
		{"known", "kilo", true, 15 * time.Millisecond, true, 15}, {"legacy", "openai", false, 0, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			withEnabledQueue(t, func() {
				ctx := kilolatency.WithUsageTTFT(context.Background(), test.duration, test.observed)
				(&usageQueuePlugin{}).HandleUsage(ctx, coreusage.Record{Provider: test.provider, Model: "fixture", RequestedAt: time.Now()})
				payload := popSinglePayload(t)
				raw, ok := payload["ttft_ms"]
				var value float64
				if ok {
					if err := json.Unmarshal(raw, &value); err != nil {
						t.Fatal(err)
					}
				}
				if ok != test.present || ok && value != test.want {
					t.Fatalf("ttft=%v present=%t", value, ok)
				}
			})
		})
	}
}
