package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func inlineSearchMessage(id, text string) map[string]any {
	return map[string]any{"type": "message", "id": id, "role": "assistant", "status": "completed", "phase": "commentary", "channel": "commentary", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{map[string]any{"type": "url_citation", "url": "https://existing.test/source", "title": "existing", "start_index": 0, "end_index": 1}}}}}
}

func inlineSearchCall(id, query string) map[string]any {
	args := ojson.NewDocString(`{"summary":"Search","extended_summary":"","destructive":false,"references":[]}`)
	code := ojson.NewDocString(`{"name":"__cpa_bps_web_search","arguments":{}}`)
	_ = code.Set("arguments.query", query)
	_ = args.Set("code", code.String())
	return map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": "run_officejs", "arguments": args.String()}
}

func inlineClientCall(id string) map[string]any {
	return map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": "run_officejs", "arguments": `{"summary":"codex2api.custom/functions.exec","extended_summary":"","destructive":false,"references":[],"code":"original client code"}`}
}

func inlineSearchResponse(output ...any) *http.Response {
	doc := ojson.NewDocString(bpsTestTerminal)
	_ = doc.Set("response.output", output)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + doc.String() + "\n\n"))}
}

func runInlineSearch(t *testing.T, ctx context.Context, raw string, stream bool, transport func(*http.Request) (*http.Response, error)) ([]byte, [][]byte, error) {
	t.Helper()
	ctx = context.WithValue(ctx, "cliproxy.roundtripper", http.RoundTripper(bpsTestTransport(transport)))
	e := NewBPSExecutor(&config.Config{BPSProtocol: config.BPSProtocolConfig{StateDir: t.TempDir()}})
	req := coreexecutor.Request{Model: "configured-model", Payload: []byte(raw)}
	opts := coreexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Metadata: map[string]any{coreexecutor.CallerScopeMetadataKey: "caller-test"}}
	if !stream {
		result, err := e.Execute(ctx, parityAuth(), req, opts)
		return result.Payload, nil, err
	}
	result, err := e.ExecuteStream(ctx, parityAuth(), req, opts)
	if err != nil {
		return nil, nil, err
	}
	var terminal []byte
	var events [][]byte
	errorsSeen := 0
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			errorsSeen++
			err = chunk.Err
		}
		for _, line := range strings.Split(string(chunk.Payload), "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			body := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			if !ojson.ValidBytes(body) {
				continue
			}
			events = append(events, body)
			if ojson.GetBytes(body, "type").String() == "response.completed" {
				terminal = []byte(ojson.GetBytes(body, "response").Raw)
			}
		}
	}
	if errorsSeen > 1 {
		t.Fatalf("multiple stream error chunks: %d", errorsSeen)
	}
	return terminal, events, err
}

func TestBPSInlineSearchDeclarations(t *testing.T) {
	for _, mode := range []string{`true`, `false`, `"indexed"`} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", mode, stream), func(t *testing.T) {
				posts := 0
				ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{Preflight: func(context.Context, coreexecutor.BPSSearchRequest) error {
					t.Error("auto declaration preflight")
					return errors.New("unconfigured")
				}, Execute: func(context.Context, coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
					t.Error("auto declaration executed search")
					return coreexecutor.BPSSearchResult{}, nil
				}})
				body, _, err := runInlineSearch(t, ctx, `{"input":"hi","tools":[{"type":"web_search","external_web_access":`+mode+`}]}`, stream, func(*http.Request) (*http.Response, error) {
					posts++
					response := inlineSearchResponse(inlineSearchMessage("msg", "hello"))
					wire, _ := io.ReadAll(response.Body)
					doc := ojson.NewDocString(strings.TrimSpace(strings.TrimPrefix(string(wire), "data: ")))
					_ = doc.Set("response.usage.cache_creation_input_tokens", 9)
					response.Body = io.NopCloser(strings.NewReader("data: " + doc.String() + "\n\n"))
					return response, nil
				})
				if err != nil || posts != 1 || ojson.GetBytes(body, "output.0.content.0.annotations.0.title").String() != "existing" || ojson.GetBytes(body, "usage.cache_creation_input_tokens").Int() != 9 {
					t.Fatalf("posts=%d err=%v body=%s", posts, err, body)
				}
			})
		}
	}
	posts := 0
	body, _, err := runInlineSearch(t, context.Background(), `{"input":"hi","tools":[{"type":"web_search","external_web_access":false}]}`, false, func(*http.Request) (*http.Response, error) {
		posts++
		return inlineSearchResponse(inlineSearchMessage("msg", "hello")), nil
	})
	if err != nil || posts != 1 || len(body) == 0 {
		t.Fatalf("no host: posts=%d err=%v", posts, err)
	}
}

func TestBPSInlineSearchRequired(t *testing.T) {
	for _, tc := range []struct {
		name, choice, extra string
		output              any
		wantErr             bool
		wantPosts           int
	}{
		{"forced-unavailable", `{"type":"web_search"}`, "", inlineSearchMessage("m", "hello"), true, 0},
		{"required-text", `"required"`, "", inlineSearchMessage("m", "hello"), true, 1},
		{"required-client", `"required"`, `,{"type":"custom","name":"functions.exec"}`, inlineClientCall("client"), false, 1},
		{"forced-function", `{"type":"function","name":"functions.exec"}`, `,{"type":"custom","name":"functions.exec"}`, inlineClientCall("client"), true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			_, _, err := runInlineSearch(t, context.Background(), `{"input":"hi","tool_choice":`+tc.choice+`,"tools":[{"type":"web_search","external_web_access":false}`+tc.extra+`]}`, false, func(*http.Request) (*http.Response, error) { posts++; return inlineSearchResponse(tc.output), nil })
			if (err != nil) != tc.wantErr || posts != tc.wantPosts {
				t.Fatalf("posts=%d err=%v", posts, err)
			}
		})
	}
}

func TestBPSInlineSearchOrderedOutput(t *testing.T) {
	for _, mode := range []struct {
		value string
		mode  coreexecutor.BPSSearchMode
	}{{`true`, coreexecutor.BPSSearchLive}, {`false`, coreexecutor.BPSSearchCached}, {`"indexed"`, coreexecutor.BPSSearchIndexed}} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", mode.mode, stream), func(t *testing.T) {
				posts, searches := 0, 0
				var session string
				ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{Execute: func(_ context.Context, req coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
					searches++
					if req.Mode != mode.mode || req.Query != "query" || req.CallerScope != "caller-test" || len(req.AllowedDomains) != 1 || req.AllowedDomains[0] != "example.com" || req.SessionID == "" {
						t.Errorf("request=%+v", req)
					}
					session = req.SessionID
					return coreexecutor.BPSSearchResult{Text: "exact\nbody https://example.com/raw [1]"}, nil
				}})
				raw := `{"input":"search","tools":[{"type":"web_search","external_web_access":` + mode.value + `,"filters":{"allowed_domains":["example.com"]}},{"type":"custom","name":"functions.exec"}]}`
				body, events, err := runInlineSearch(t, ctx, raw, stream, func(req *http.Request) (*http.Response, error) {
					posts++
					if posts == 1 {
						return inlineSearchResponse(inlineSearchMessage("before", "same text"), map[string]any{"type": "reasoning", "id": "reasoning-private", "encrypted_content": "secret"}, inlineSearchCall("search-1", "query"), inlineSearchMessage("after", "same text")), nil
					}
					wire, _ := io.ReadAll(req.Body)
					found := false
					for _, item := range ojson.GetBytes(wire, "input").Array() {
						if item.Get("type").String() == "function_call_output" && item.Get("call_id").String() == "search-1" {
							result := ojson.Parse(item.Get("output").String())
							found = result.Get("text").String() == "exact\nbody https://example.com/raw [1]" && !result.Get("sources").Exists()
						}
					}
					if !found || strings.Contains(string(wire), `"type":"web_search_call"`) {
						t.Errorf("native continuation lost result or reused public items: %s", wire)
					}
					return inlineSearchResponse(inlineSearchMessage("final", "final text"), inlineClientCall("client")), nil
				})
				if err != nil || posts != 2 || searches != 1 || session == "" {
					t.Fatalf("posts=%d searches=%d err=%v body=%s", posts, searches, err, body)
				}
				output := ojson.GetBytes(body, "output").Array()
				if len(output) != 5 {
					t.Fatalf("output=%s", body)
				}
				for i, want := range []string{"before", "", "after", "final", ""} {
					if want != "" && output[i].Get("id").String() != want {
						t.Fatalf("index %d: output=%s", i, body)
					}
				}
				if output[1].Get("type").String() != "web_search_call" || output[1].Get("action.sources").Exists() || output[4].Get("name").String() != "functions.exec" || strings.Contains(string(body), "reasoning-private") || strings.Contains(string(body), "__cpa_bps_web_search") {
					t.Fatalf("output=%s", body)
				}
				for _, index := range []int{0, 2, 3} {
					if output[index].Get("phase").String() != "commentary" || output[index].Get("channel").String() != "commentary" || output[index].Get("content.0.annotations.0.title").String() != "existing" {
						t.Fatalf("message lost fields: %s", output[index].Raw)
					}
				}
				if stream {
					completed := 0
					done := map[int]int{}
					for _, event := range events {
						root := ojson.ParseBytes(event)
						switch root.Get("type").String() {
						case "response.completed":
							completed++
						case "response.output_item.done":
							index := int(root.Get("output_index").Int())
							done[index]++
							if root.Get("item.id").String() != output[index].Get("id").String() {
								t.Fatalf("event/output disagree: %s", event)
							}
						}
					}
					if completed != 1 || len(done) != 5 {
						t.Fatalf("completed=%d done=%v", completed, done)
					}
					for index, count := range done {
						if count != 1 {
							t.Fatalf("index %d done=%d", index, count)
						}
					}
				}
			})
		}
	}
}

func TestBPSInlineSearchFailures(t *testing.T) {
	for _, kind := range []string{coreexecutor.BPSSearchBackendUnavailable, coreexecutor.BPSSearchNoCredential, coreexecutor.BPSSearchHTTPRejected, coreexecutor.BPSSearchInvalidResponse, coreexecutor.BPSSearchPolicyRejected} {
		t.Run(kind, func(t *testing.T) {
			posts, searches := 0, 0
			ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{Execute: func(context.Context, coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
				searches++
				return coreexecutor.BPSSearchResult{}, &coreexecutor.BPSSearchError{Kind: kind, Status: 403}
			}})
			body, events, err := runInlineSearch(t, ctx, `{"input":"search","tools":[{"type":"web_search","external_web_access":false}]}`, true, func(*http.Request) (*http.Response, error) {
				posts++
				return inlineSearchResponse(inlineSearchCall("call", "q")), nil
			})
			if err == nil || posts != 1 || searches != 1 || len(body) != 0 || !strings.Contains(err.Error(), kind) {
				t.Fatalf("posts=%d searches=%d err=%v", posts, searches, err)
			}
			var denial interface{ BPSPermissionDenial() (string, int, bool) }
			if errors.As(err, &denial) {
				source, _, _ := denial.BPSPermissionDenial()
				if source != "" {
					t.Fatalf("search attributed to BPS: %s", source)
				}
			}
			for _, event := range events {
				if strings.Contains(string(event), "response.completed") || strings.Contains(string(event), "response.web_search_call.completed") {
					t.Fatalf("failure emitted success: %s", event)
				}
			}
		})
	}
}

func TestBPSInlineSearchFinalToolOrder(t *testing.T) {
	for _, toolFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(toolFirst), func(t *testing.T) {
			items := []any{inlineSearchMessage("message", "text"), inlineClientCall("client")}
			if toolFirst {
				items[0], items[1] = items[1], items[0]
			}
			body, events, err := runInlineSearch(t, context.Background(), `{"input":"hi","tools":[{"type":"web_search","external_web_access":false},{"type":"custom","name":"functions.exec"}]}`, true, func(*http.Request) (*http.Response, error) { return inlineSearchResponse(items...), nil })
			if err != nil {
				t.Fatal(err)
			}
			messageIndex, toolIndex := 0, 1
			if toolFirst {
				messageIndex, toolIndex = 1, 0
			}
			output := ojson.GetBytes(body, "output").Array()
			if len(output) != 2 || output[messageIndex].Get("id").String() != "message" || output[toolIndex].Get("name").String() != "functions.exec" || output[toolIndex].Get("call_id").String() == "" {
				t.Fatal(string(body))
			}
			for _, event := range events {
				root := ojson.ParseBytes(event)
				if !root.Get("output_index").Exists() {
					continue
				}
				index := root.Get("output_index").Int()
				id := root.Get("item_id").String()
				if id == "" {
					id = root.Get("item.id").String()
				}
				if id != "" && id != output[index].Get("id").String() {
					t.Fatalf("ID/index changed: %s", event)
				}
			}
		})
	}
}

func TestBPSInlineSearchForcedPreflight(t *testing.T) {
	posts, preflights, searches := 0, 0, 0
	ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{
		Preflight: func(_ context.Context, req coreexecutor.BPSSearchRequest) error {
			preflights++
			if req.Mode != coreexecutor.BPSSearchCached || req.CallerScope != "caller-test" || req.Query != "" {
				t.Errorf("preflight=%+v", req)
			}
			return nil
		},
		Execute: func(context.Context, coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
			searches++
			return coreexecutor.BPSSearchResult{Text: "body"}, nil
		},
	})
	_, _, err := runInlineSearch(t, ctx, `{"input":"search","tool_choice":{"type":"web_search"},"tools":[{"type":"web_search","external_web_access":false}]}`, false, func(*http.Request) (*http.Response, error) {
		posts++
		if posts == 1 {
			return inlineSearchResponse(inlineSearchCall("call", "q")), nil
		}
		return inlineSearchResponse(inlineSearchMessage("m", "done")), nil
	})
	if err != nil || posts != 2 || preflights != 1 || searches != 1 {
		t.Fatalf("posts=%d preflights=%d searches=%d err=%v", posts, preflights, searches, err)
	}
}

func TestBPSInlineSearchNoRestrictedLegacyFallback(t *testing.T) {
	for _, mode := range []string{`false`, `"indexed"`} {
		t.Run(mode, func(t *testing.T) {
			posts, legacy := 0, 0
			ctx := coreexecutor.WithBPSWebSearch(context.Background(), func(context.Context, []byte) ([]byte, error) { legacy++; return nil, nil })
			_, _, err := runInlineSearch(t, ctx, `{"input":"search","tools":[{"type":"web_search","external_web_access":`+mode+`}]}`, false, func(*http.Request) (*http.Response, error) {
				posts++
				return inlineSearchResponse(inlineSearchCall("call", "q")), nil
			})
			if err == nil || posts != 1 || legacy != 0 || !strings.Contains(err.Error(), "backend_unavailable") {
				t.Fatalf("posts=%d legacy=%d err=%v", posts, legacy, err)
			}
		})
	}
}

func TestBPSInlineSearchMixedBatchAndContinuationFailure(t *testing.T) {
	for _, scenario := range []string{"client-mixed", "compaction-mixed", "continuation-403"} {
		t.Run(scenario, func(t *testing.T) {
			posts, searches := 0, 0
			ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{Execute: func(context.Context, coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
				searches++
				return coreexecutor.BPSSearchResult{Text: "body"}, nil
			}})
			body, _, err := runInlineSearch(t, ctx, `{"input":"search","tools":[{"type":"web_search","external_web_access":false},{"type":"custom","name":"functions.exec"}]}`, true, func(*http.Request) (*http.Response, error) {
				posts++
				if posts > 1 {
					return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("denied"))}, nil
				}
				items := []any{inlineSearchCall("call", "q")}
				if scenario == "client-mixed" {
					items = append(items, inlineClientCall("client"))
				}
				if scenario == "compaction-mixed" {
					items = append(items, map[string]any{"type": "compaction", "id": "compaction", "encrypted_content": "test"})
				}
				return inlineSearchResponse(items...), nil
			})
			wantPosts, wantSearch := 1, 0
			if scenario == "continuation-403" {
				wantPosts, wantSearch = 2, 1
			}
			if err == nil || posts != wantPosts || searches != wantSearch || len(body) != 0 {
				t.Fatalf("posts=%d searches=%d err=%v body=%s", posts, searches, err, body)
			}
			if scenario == "continuation-403" {
				var denial interface{ BPSPermissionDenial() (string, int, bool) }
				if !errors.As(err, &denial) {
					t.Fatal("lost continuation denial")
				}
				source, status, _ := denial.BPSPermissionDenial()
				if source != "http" || status != 403 {
					t.Fatalf("source=%s status=%d", source, status)
				}
			}
		})
	}
}

func TestBPSInlineSearchSessionAndBudget(t *testing.T) {
	var priorSession string
	for _, count := range []int{2, 5} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			posts, searches := 0, 0
			var session string
			ctx := coreexecutor.WithBPSWebSearchBackend(context.Background(), coreexecutor.BPSWebSearchBackend{Execute: func(_ context.Context, req coreexecutor.BPSSearchRequest) (coreexecutor.BPSSearchResult, error) {
				searches++
				if session == "" {
					session = req.SessionID
				}
				if session != req.SessionID || session == priorSession || session == "" {
					t.Errorf("session changed or inherited: %q", req.SessionID)
				}
				return coreexecutor.BPSSearchResult{Text: "result"}, nil
			}})
			_, _, err := runInlineSearch(t, ctx, `{"input":"search","tools":[{"type":"web_search","external_web_access":false}]}`, false, func(*http.Request) (*http.Response, error) {
				posts++
				if posts <= count {
					return inlineSearchResponse(inlineSearchCall(fmt.Sprint("call-", posts), "q")), nil
				}
				return inlineSearchResponse(inlineSearchMessage("m", "done")), nil
			})
			if count == 2 && (err != nil || posts != 3 || searches != 2) {
				t.Fatalf("posts=%d searches=%d err=%v", posts, searches, err)
			}
			if count == 5 && (err == nil || posts != 5 || searches != 4) {
				t.Fatalf("posts=%d searches=%d err=%v", posts, searches, err)
			}
			priorSession = session
		})
	}
}
