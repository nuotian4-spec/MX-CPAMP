package executor

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestRequestLatencyActualBPSPrepare(t *testing.T) {
	ctx, state := requestlatency.NewContext(context.Background())
	executor := NewBPSExecutor(&config.Config{})
	auth := &coreauth.Auth{ID: "private-auth", Provider: "bps", Metadata: map[string]any{"type": "bps", "access_token": "private-token", "account_id": "private-account"}}
	raw := []byte("{\"model\":\"configured-model\",\"input\":\"secret-prompt\"}")
	_, err := executor.build(ctx, auth, coreexecutor.Request{Model: "configured-model", Payload: raw}, coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, OriginalRequest: raw})
	if err != nil {
		t.Fatal(err)
	}
	fields := state.Finish(nil, 200)
	if fields["bps_prepare_count"] != 1 || fields["executor_builtin_bps_count"] != 1 {
		t.Fatalf("prepare hook: %v", fields)
	}
	if _, ok := fields["bps_upload_ms"]; ok {
		t.Fatalf("text request invented upload: %v", fields)
	}
}
