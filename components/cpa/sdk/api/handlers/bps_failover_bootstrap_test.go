package handlers

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"net/http"
	"testing"
)

type bpsBootstrapExecutor struct{ bootstrapStreamExecutor }

func (*bpsBootstrapExecutor) Identifier() string { return "bps" }

type bpsAcceptedBootstrapError struct{}

func (bpsAcceptedBootstrapError) Error() string           { return "synthetic accepted stream failed" }
func (bpsAcceptedBootstrapError) StatusCode() int         { return http.StatusBadGateway }
func (bpsAcceptedBootstrapError) IsStreamTruncated() bool { return true }

func TestBPSFailoverBootstrapAcceptedStop(t *testing.T) {
	e := &bpsBootstrapExecutor{}
	e.stream = func(ctx context.Context, call int) (*coreexecutor.StreamResult, error) {
		coreexecutor.MarkUpstreamAttempt(ctx)
		ch := make(chan coreexecutor.StreamChunk, 2)
		ch <- coreexecutor.StreamChunk{Payload: []byte(`data: {"type":`)}
		ch <- coreexecutor.StreamChunk{Err: bpsAcceptedBootstrapError{}}
		close(ch)
		return &coreexecutor.StreamResult{Chunks: ch}, nil
	}
	m := coreauth.NewManager(nil, nil, nil)
	m.RegisterExecutor(e)
	for _, id := range []string{"failover-A", "failover-B", "failover-C"} {
		a := &coreauth.Auth{ID: id, Provider: "bps", Status: coreauth.StatusActive}
		if _, err := m.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "bps", []*registry.ModelInfo{{ID: "bps-failover-model"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Streaming: sdkconfig.StreamingConfig{BootstrapRetries: 3}}, m)
	data, _, errs := h.ExecuteStreamWithAuthManager(context.Background(), "openai-response", "bps-failover-model", []byte(`{"model":"bps-failover-model","input":"hello"}`), "")
	if data != nil {
		for range data {
		}
	}
	for msg := range errs {
		t.Logf("result error: %+v", msg)
	}
	t.Logf("actual generations=%d; wanted A=1/B=0/C=0", e.Calls())
	if e.Calls() != 1 {
		t.Fatalf("accepted BPS stream was replayed: generations=%d", e.Calls())
	}
}
