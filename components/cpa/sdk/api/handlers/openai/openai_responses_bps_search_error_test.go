package openai

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestForwardResponsesStreamBPSSearchFailureIdentity(t *testing.T) {
	for _, scenario := range []string{"search_created", "bounded_search_created", "search_no_created", "ordinary_codex_created", "ordinary_codex_search_text", "ordinary_codex_web_search", "bps_continuation"} {
		t.Run(scenario, func(t *testing.T) {
			h, recorder, c, flusher := newResponsesStreamTestHandler(t)
			c.Request.Header.Set("User-Agent", "Codex Desktop/26.803.41515")
			framer := &responsesSSEFramer{}
			responseID := "resp_delivered"
			if scenario == "bps_continuation" {
				responseID = "resp_bps_delivered"
			}
			if scenario != "search_no_created" {
				doc := ojson.NewDocString(`{"type":"response.created","response":{}}`)
				_ = doc.Set("response.id", responseID)
				framer.WriteChunk(c.Writer, []byte("event: response.created\ndata: "+doc.String()+"\n\n"))
			}
			if scenario == "ordinary_codex_web_search" || scenario == "bps_continuation" {
				doc := ojson.NewDocString(`{"type":"response.output_item.added","item":{"type":"web_search_call"}}`)
				_ = doc.Set("response_id", responseID)
				framer.WriteChunk(c.Writer, []byte("data: "+doc.String()+"\n\n"))
			}
			var failure error = &coreexecutor.BPSSearchError{Kind: coreexecutor.BPSSearchHTTPRejected, Status: http.StatusForbidden}
			if scenario == "bounded_search_created" {
				failure = helps.BPSSearchFailure(failure)
			}
			if scenario == "ordinary_codex_created" || scenario == "ordinary_codex_web_search" || scenario == "bps_continuation" {
				failure = errors.New("ordinary Codex upstream failure")
			}
			if scenario == "ordinary_codex_search_text" {
				failure = errors.New(`{"error":{"message":"BPS response failed","diagnostics":{"stage":"web_search"}}}`)
			}
			errs := make(chan *interfaces.ErrorMessage, 1)
			errs <- &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: failure}
			close(errs)
			h.forwardResponsesStream(c, flusher, func(error) {}, make(chan []byte), errs, framer)
			body := recorder.Body.String()
			if strings.Count(body, "event: response.failed") != 1 {
				t.Fatalf("expected one failed event: %s", body)
			}
			var failed string
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(line, "data: ") && ojson.Get(strings.TrimPrefix(line, "data: "), "type").String() == "response.failed" {
					failed = strings.TrimPrefix(line, "data: ")
				}
			}
			if scenario == "search_created" || scenario == "bounded_search_created" || scenario == "bps_continuation" {
				if ojson.Get(failed, "response.id").String() != responseID || ojson.Get(failed, "response_id").String() != responseID {
					t.Fatalf("search failed ID mismatch: %s", failed)
				}
			} else {
				want := handlers.BuildOpenAIResponsesStreamFailedChunk(http.StatusBadGateway, failure.Error(), framer.dataFrames)
				if failed != string(want) {
					t.Fatalf("unrelated error shape changed: got=%s want=%s", failed, want)
				}
			}
		})
	}
}
