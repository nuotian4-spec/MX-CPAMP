package helps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/basispoints"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestBPSWebSearchConstraintsAndHistory(t *testing.T) {
	for _, extra := range []string{`,"user_location":{}`, `,"filters":{"excluded_domains":["example.com"]}`, `,"external_web_access":null`, `,"external_web_access":"cached"`, `,"external_web_access":"live"`, `,"external_web_access":1`, `,"filters":{"allowed_domains":"example.com"}`} {
		doc := ojson.NewDocString(`{"tools":[{"type":"web_search"` + extra + `}],"input":"hi"}`)
		if _, err := PrepareBPSSearch(doc, true); err == nil {
			t.Errorf("accepted constraint %s", extra)
		}
	}
	doc := ojson.NewDocString(`{"tools":[{"type":"tool_search"}],"input":[{"type":"web_search_call","action":{"query":"old query","sources":[{"url":"https://example.com/source"}]}},{"role":"user","content":"new task"}]}`)
	s, err := PrepareBPSSearch(doc, true)
	if err != nil || s != nil || !strings.Contains(doc.String(), "result body is not available") || strings.Contains(doc.String(), "__cpa_bps_web_search") {
		t.Fatalf("history=%s err=%v", doc.String(), err)
	}
	for _, kind := range []string{"web_search", "web_search_preview", "web_search_preview_2025_03_11", "web_search_2025_08_26"} {
		doc = ojson.NewDocString(`{"tools":[{"type":"` + kind + `"}],"tool_choice":{"type":"` + kind + `"},"input":"hi"}`)
		s, err = PrepareBPSSearch(doc, true)
		if err != nil || s == nil || !s.Required || doc.Get("tool_choice").String() != "auto" {
			t.Fatalf("forced %s: %v", kind, err)
		}
	}
	doc = ojson.NewDocString(`{"tools":[{"type":"function","name":"client"}],"tool_choice":"required","input":"hi"}`)
	if s, err = PrepareBPSSearch(doc, true); err != nil || s != nil || doc.Get("tool_choice").String() != "required" {
		t.Fatal("ordinary choice changed")
	}
}

func TestBPSInlineSearchModeDeclarations(t *testing.T) {
	for _, tc := range []struct {
		value    string
		mode     coreexecutor.BPSSearchMode
		explicit bool
	}{
		{"", coreexecutor.BPSSearchLive, false}, {`,"external_web_access":true`, coreexecutor.BPSSearchLive, true},
		{`,"external_web_access":false`, coreexecutor.BPSSearchCached, true}, {`,"external_web_access":"indexed"`, coreexecutor.BPSSearchIndexed, true},
	} {
		doc := ojson.NewDocString(`{"input":"hi","include":["web_search_call.action.sources"],"tools":[{"type":"web_search"` + tc.value + `}]}`)
		s, err := PrepareBPSSearch(doc, true)
		if err != nil || s == nil || s.Mode != tc.mode || s.AccessExplicit != tc.explicit || !s.RequireSources || s.SessionID == "" {
			t.Fatalf("%s: session=%+v err=%v", tc.value, s, err)
		}
	}
	if _, err := PrepareBPSSearch(ojson.NewDocString(`{"tools":[{"type":"web_search"},{"type":"web_search"}]}`), true); err == nil {
		t.Fatal("duplicate declaration accepted")
	}
}

func TestBPSInlineSearchTypedResults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   coreexecutor.BPSSearchResult
		required bool
		wantErr  bool
		sources  bool
	}{
		{"text", coreexecutor.BPSSearchResult{Text: "exact\nhttps://example.com/raw [1]"}, false, false, false},
		{"empty", coreexecutor.BPSSearchResult{Text: ""}, false, false, false},
		{"known-empty", coreexecutor.BPSSearchResult{SourcesProvided: true}, false, false, true},
		{"source", coreexecutor.BPSSearchResult{Text: "answer", SourcesProvided: true, Sources: []coreexecutor.BPSSearchSource{{URL: "https://sub.example.com/a", Title: "a"}}}, true, false, true},
		{"required-missing", coreexecutor.BPSSearchResult{}, true, true, false},
		{"bad-url", coreexecutor.BPSSearchResult{SourcesProvided: true, Sources: []coreexecutor.BPSSearchSource{{URL: "javascript:alert(1)"}}}, false, true, false},
		{"bad-domain", coreexecutor.BPSSearchResult{SourcesProvided: true, Sources: []coreexecutor.BPSSearchSource{{URL: "https://other.test/a"}}}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &BPSSearchSession{RequireSources: tc.required, Domains: []string{"example.com"}}
			body, _, err := s.TypedResult(tc.result, "query")
			if (err != nil) != tc.wantErr {
				t.Fatalf("body=%s err=%v", body, err)
			}
			if err == nil && (ojson.GetBytes(body, "text").String() != tc.result.Text || ojson.GetBytes(body, "sources").Exists() != tc.sources) {
				t.Fatalf("body=%s", body)
			}
		})
	}
}

func TestBPSInlineSearchPreservesExistingAnnotations(t *testing.T) {
	for _, searched := range []bool{false, true} {
		s := &BPSSearchSession{ResponseID: "resp_parent"}
		if searched {
			s.Sources = []map[string]any{{"url": "https://example.com/a", "title": "a"}}
		}
		event := []byte(`{"type":"response.completed","response":{"id":"resp_upstream","output":[{"type":"message","id":"m","content":[{"type":"output_text","text":"existing https://example.com/a","annotations":[{"type":"url_citation","url":"https://existing.test","title":"old","start_index":0,"end_index":8}]}]}]}}`)
		out := ojson.ParseBytes(s.FinalEvent(event))
		annotations := out.Get("response.output.0.content.0.annotations").Array()
		count := 1
		if searched {
			count = 2
		}
		if len(annotations) != count || annotations[0].Get("url").String() != "https://existing.test" {
			t.Fatal(out.Raw)
		}
	}
}

func TestBPSInlineSearchBudgetAndIdentity(t *testing.T) {
	s := &BPSSearchSession{Calls: map[string]bool{"previous": true}}
	for _, calls := range [][]basispoints.WebSearchCall{
		{{ID: "same"}, {ID: "same"}}, {{ID: "previous"}}, {{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}},
	} {
		if err := s.CheckBatch(calls); err == nil {
			t.Fatalf("accepted calls %+v", calls)
		}
	}
	if err := s.CheckBatch([]basispoints.WebSearchCall{{ID: "1"}, {ID: "2"}, {ID: "3"}}); err != nil {
		t.Fatal(err)
	}
}

func TestBPSInlineSearchDisabledDeclarations(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, choice := range []string{`"none"`, `{"type":"web_search"}`} {
			doc := ojson.NewDocString(`{"tools":[{"type":"web_search","external_web_access":false}],"tool_choice":` + choice + `}`)
			s, err := PrepareBPSSearch(doc, enabled)
			if choice == `"none"` || !enabled {
				if choice != `"none"` && err == nil {
					t.Fatal("disabled forced search accepted")
				}
				if choice == `"none"` && (err != nil || s != nil || doc.Get("tools.#").Int() != 0) {
					t.Fatalf("none: session=%+v err=%v", s, err)
				}
			}
		}
	}
}

func TestBPSInlineSearchCitationMergeDeduplicatesSources(t *testing.T) {
	s := &BPSSearchSession{Sources: []map[string]any{{"url": "https://example.com/a", "title": "a"}, {"url": "https://example.com/a", "title": "a"}}}
	event := []byte(`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"https://example.com/a","annotations":[]}]}]}}`)
	first := s.FinalEvent(event)
	second := s.FinalEvent(first)
	if ojson.GetBytes(first, "response.output.0.content.0.annotations.#").Int() != 1 || ojson.GetBytes(second, "response.output.0.content.0.annotations.#").Int() != 1 {
		t.Fatalf("first=%s second=%s", first, second)
	}
}

func TestBPSWebSearchCancellationHoldsCallbackSlots(t *testing.T) {
	if len(bpsSearchSlots) != 0 {
		t.Fatal("dirty search slots")
	}
	release := make(chan struct{})
	var returned sync.WaitGroup
	defer func() { close(release); returned.Wait() }()
	for i := 0; i < cap(bpsSearchSlots); i++ {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		done := make(chan error, 1)
		s := &BPSSearchSession{Calls: map[string]bool{}, ResponseID: "resp_test"}
		go func() {
			returned.Add(1)
			_, err := s.Run(ctx, func(ctx context.Context, _ []byte) ([]byte, error) {
				defer returned.Done()
				if _, ok := ctx.Deadline(); ok {
					t.Error("search added a deadline")
				}
				close(started)
				<-release
				return nil, ctx.Err()
			}, basispoints.WebSearchCall{ID: "call", Query: "q"}, func([]byte) error { return nil })
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	}
	s := &BPSSearchSession{Calls: map[string]bool{}}
	_, err := s.Run(context.Background(), func(context.Context, []byte) ([]byte, error) { t.Error("17th callback ran"); return nil, nil }, basispoints.WebSearchCall{ID: "overflow", Query: "q"}, func([]byte) error { return nil })
	if err == nil || len(bpsSearchSlots) != 16 {
		t.Fatalf("slots=%d err=%v", len(bpsSearchSlots), err)
	}
}

func TestBPSWebSearchCitationsUseActualUnicodeURL(t *testing.T) {
	s := &BPSSearchSession{Sources: []map[string]any{{"url": "https://example.com/source", "title": "source"}}}
	for _, tc := range []struct {
		text  string
		count int
	}{{"中文🙂 https://example.com/source", 1}, {"https://example.com/source-not-real", 0}, {"https://example.com/source.pdf", 0}, {"https://example.com/source?changed=1", 0}, {"no URL", 0}} {
		doc := ojson.NewDocString(`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text"}]}]}}`)
		_ = doc.Set("response.output.0.content.0.text", tc.text)
		out := s.FinalEvent(doc.Bytes())
		annotations := ojson.GetBytes(out, "response.output.0.content.0.annotations").Array()
		if len(annotations) != tc.count {
			t.Fatalf("%q: %s", tc.text, out)
		}
		if tc.count == 1 && (annotations[0].Get("start_index").Int() != 4 || annotations[0].Get("end_index").Int() != 30) {
			t.Fatalf("unicode offsets=%s", out)
		}
	}
}
