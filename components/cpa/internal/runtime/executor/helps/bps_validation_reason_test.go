package helps

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
)

func TestBPSValidationReasonPropagation(t *testing.T) {
	for _, reason := range []string{"schema_mismatch", "nesting_limit", "number_precision_limit", "number_exponent_limit", "number_budget_limit", "PRIVATE_REASON"} {
		t.Run(reason, func(t *testing.T) {
			f := &basispoints.BridgeFailure{Message: "local validation failed", FailureStage: "bridge_tool_validation", Reason: "schema_validation", ValidationReason: reason, SchemaTool: "echo", SchemaPath: "$", UpstreamTerminalEvent: "response.completed", ToolBatchWithheld: true}
			raw, _ := json.Marshal(map[string]any{"type": "response.failed", "response": map[string]any{"error": f.Details()}})
			for _, stream := range []bool{false, true} {
				var err error
				if stream {
					err = ReadBPSEvents(context.Background(), &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(raw) + "\n\n"))}, func([]byte) error { t.Fatal("failed event published as success"); return nil })
				} else {
					err = BPSToolValidationFailure(context.Background(), f, raw)
				}
				body := BPSFailureBody(WithBPSHTTPStatus(err, 200), 502)
				got := ojson.Parse(body).Get("error.diagnostics.validation_reason").String()
				want := basispoints.BoundedToolValidationReason(reason)
				if want != "schema_mismatch" && want != "" && ojson.Parse(body).Get("error.diagnostics.rule").String() != "" {
					t.Fatal("resource diagnostic fabricated schema rule")
				}
				if got != want || strings.Contains(body, "PRIVATE") {
					t.Fatalf("body=%s", body)
				}
				if ojson.Parse(body).Get("error.diagnostics.reason").String() != "schema_validation" || !ojson.Parse(body).Get("error.diagnostics.tool_batch_withheld").Bool() {
					t.Fatal(body)
				}
			}
		})
	}
}
