package helps

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilolatency"
)

// ObserveKiloLatency runs before synchronous ownership persistence.
func ObserveKiloLatency(ctx context.Context, event []byte) {
	state := kilolatency.From(ctx)
	text, tool := kilolatency.Content(event)
	if text {
		state.Mark("text")
	}
	if tool {
		state.Tool()
	}
}
