package helps

import (
	"errors"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestBPSFailureDiagnosticsWhitelist(t *testing.T) {
	for _, err := range []error{errors.New("PRIVATE_BODY_MARKER"), NewBPSFailure("PRIVATE_STAGE", "PRIVATE_CODE")} {
		body := BPSFailureBody(WithBPSHTTPStatus(err, 600), 502)
		root := ojson.Parse(body)
		if strings.Contains(body, "PRIVATE_") || root.Get("error.diagnostics.stage").String() != "unknown" || root.Get("error.diagnostics.code").String() != "unknown" {
			t.Fatal("failure diagnostics accepted unbounded strings")
		}
		if root.Get("error.diagnostics.upstream_http_status").Raw != "null" || root.Get("error.diagnostics.inband_status").Raw != "null" {
			t.Fatal("unknown or invalid status was published")
		}
	}
}
