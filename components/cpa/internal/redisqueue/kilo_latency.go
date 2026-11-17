package redisqueue

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

func usageTTFTMilliseconds(ctx context.Context, record coreusage.Record) *int64 {
	duration := record.TTFT
	if record.Provider == "kilo" {
		var observed bool
		duration, observed = kilolatency.UsageTTFT(ctx)
		if !observed {
			return nil
		}
	}
	ms := duration.Milliseconds()
	return &ms
}
