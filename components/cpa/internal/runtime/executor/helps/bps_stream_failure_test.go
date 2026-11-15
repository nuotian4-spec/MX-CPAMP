package helps

import (
	"context"
	"errors"
	"fmt"
	"testing"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestBPSStreamFailureRetainsConfirmedDenialAfterCancel(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%v", full), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			chunks := make(chan coreexecutor.StreamChunk, 1)
			if full {
				chunks <- coreexecutor.StreamChunk{Payload: []byte("queued-payload")}
			}
			failure := &BPSFailure{stage: "upstream_inband", code: "upstream_failed", upstreamHTTP: 200, inbandStatus: 403}
			err := fmt.Errorf("accepted: %w", failure)
			SendBPSStreamFailure(ctx, chunks, err)
			close(chunks)
			count := 0
			for chunk := range chunks {
				count++
				if !errors.Is(chunk.Err, failure) {
					t.Fatalf("confirmed denial lost: %+v", chunk)
				}
			}
			if count != 1 {
				t.Fatalf("terminal count=%d", count)
			}
		})
	}
}

func TestBPSStreamFailureDoesNotPromoteCancellationOrText(t *testing.T) {
	for _, err := range []error{context.Canceled, errors.New("HTTP 403 permission denied"), NewBPSFailure("upstream_inband", "unknown")} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		chunks := make(chan coreexecutor.StreamChunk, 1)
		chunks <- coreexecutor.StreamChunk{Payload: []byte("queued-payload")}
		SendBPSStreamFailure(ctx, chunks, err)
		if got := <-chunks; got.Err != nil || string(got.Payload) != "queued-payload" {
			t.Fatalf("unconfirmed error replaced payload: %+v", got)
		}
	}
}

func TestBPSStreamFailurePreservesLivePayloadOrder(t *testing.T) {
	chunks := make(chan coreexecutor.StreamChunk, 1)
	chunks <- coreexecutor.StreamChunk{Payload: []byte("queued-payload")}
	failure := &BPSFailure{stage: "upstream_inband", code: "upstream_failed", upstreamHTTP: 200, inbandStatus: 403}
	done := make(chan struct{})
	go func() {
		SendBPSStreamFailure(context.Background(), chunks, failure)
		close(chunks)
		close(done)
	}()
	if got := <-chunks; string(got.Payload) != "queued-payload" || got.Err != nil {
		t.Fatalf("live payload discarded: %+v", got)
	}
	if got := <-chunks; got.Err != failure {
		t.Fatalf("terminal failure lost: %+v", got)
	}
	<-done
}
