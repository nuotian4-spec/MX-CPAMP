package helps

import (
	"context"
	"errors"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// SendBPSStreamFailure is called by the sole producer of a capacity-one channel.
// A confirmed refusal must survive cancellation even if the consumer has left.
func SendBPSStreamFailure(ctx context.Context, chunks chan coreexecutor.StreamChunk, err error) {
	chunk := coreexecutor.StreamChunk{Err: err}
	select {
	case chunks <- chunk:
	case <-ctx.Done():
		var failure interface{ BPSPermissionDenial() (string, int, bool) }
		if !errors.As(err, &failure) {
			return
		}
		if source, _, _ := failure.BPSPermissionDenial(); source == "" {
			return
		}
		// Only an already-canceled consumer can lose its queued payload. No other
		// producer can fill the slot between this drain and the terminal send.
		select {
		case <-chunks:
		default:
		}
		chunks <- chunk
	}
}
