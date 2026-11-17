package helps

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/requestlatency"
)

func TestRequestLatencyActualBPSUpload(t *testing.T) {
	bpsAttachmentIDs = basispoints.AttachmentCache{}
	ctx, state := requestlatency.NewContext(context.Background())
	calls := 0
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", bpsImageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-private\"}"))}, nil
	}))
	wire, _, _, err := PrepareBPSImages(ctx, &config.Config{}, bpsTestAuth(), bpsImageBody(t, bpsImagePart(bpsTestImage(t, 39))), "")
	if err != nil || len(wire) == 0 {
		t.Fatalf("image preparation: %v", err)
	}
	fields := state.Finish(nil, 200)
	if calls != 1 || fields["bps_upload_count"] != 1 {
		t.Fatalf("upload hook: calls=%d fields=%v", calls, fields)
	}
}
