package helps

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type kiloAlphaContractKey struct{}
type kiloAlphaContract struct {
	query  KiloAlphaQuery
	budget int64
}

type kiloAlphaResultError struct{ error }

func (e kiloAlphaResultError) StatusCode() int       { return 502 }
func (e kiloAlphaResultError) IsRequestScoped() bool { return true }
func (e kiloAlphaResultError) ErrorCode() string     { return strings.SplitN(e.Error(), ":", 2)[0] }

// ValidateKiloAlphaTerminal makes an invalid business result fail the one
// execution result rather than recording a successful search based on HTTP 200.
func ValidateKiloAlphaTerminal(ctx context.Context, raw []byte) error {
	if _, ok := ctx.Value(kiloAlphaContractKey{}).(kiloAlphaContract); !ok {
		return nil
	}
	_, err := KiloAlphaResult(raw)
	if err != nil {
		return kiloAlphaResultError{err}
	}
	return nil
}

// WithKiloAlphaContract carries an already accepted query to the final barrier.
func WithKiloAlphaContract(ctx context.Context, query KiloAlphaQuery, budget int64) context.Context {
	query.Domains = append([]string(nil), query.Domains...)
	return context.WithValue(ctx, kiloAlphaContractKey{}, kiloAlphaContract{query: query, budget: budget})
}

// ValidateKiloAlphaPayload never restores fields removed by payload rules.
func ValidateKiloAlphaPayload(ctx context.Context, doc *ojson.Doc, upstreamModel string) error {
	contract, ok := ctx.Value(kiloAlphaContractKey{}).(kiloAlphaContract)
	if !ok {
		return nil
	}
	conflict := func() error {
		return kiloAlphaInvalid("kilo_alpha_payload_conflict", "Final payload conflicts with the accepted Alpha search contract")
	}
	if doc.Get("model").String() != "openai/"+strings.TrimPrefix(upstreamModel, "openai/") || doc.Get("tool_choice.type").String() != "web_search" {
		return conflict()
	}
	tools := doc.Get("tools").Array()
	if len(tools) != 1 || tools[0].Get("type").String() != "web_search" {
		return conflict()
	}
	access := tools[0].Get("external_web_access")
	if (access.Type != ojson.True && access.Type != ojson.False) || access.Bool() != contract.query.ExternalWebAccess {
		return conflict()
	}
	domains := tools[0].Get("filters.allowed_domains")
	if len(contract.query.Domains) == 0 {
		if tools[0].Get("filters").Exists() {
			return conflict()
		}
	} else {
		expected := make([]any, len(contract.query.Domains))
		for i, d := range contract.query.Domains {
			expected[i] = d
		}
		if !reflect.DeepEqual(domains.Value(), expected) || len(tools[0].Get("filters").Map()) != 1 {
			return conflict()
		}
	}
	budget := doc.Get("max_output_tokens")
	if budget.Type != ojson.Number || budget.Int() <= 0 || budget.Int() > contract.budget || budget.Float() != float64(budget.Int()) {
		return conflict()
	}
	includeSources := false
	for _, item := range doc.Get("include").Array() {
		includeSources = includeSources || item.String() == "web_search_call.action.sources"
	}
	if !includeSources {
		return conflict()
	}
	return nil
}

// KiloAlphaResult formats actual search output without fabricating citations.
func KiloAlphaResult(raw []byte) (string, error) {
	if !ojson.ValidBytes(raw) {
		return "", fmt.Errorf("kilo_alpha_invalid_result: invalid response JSON")
	}
	root := ojson.ParseBytes(raw)
	if root.Get("response").IsObject() {
		root = root.Get("response")
	}
	if root.Get("status").String() != "completed" || (root.Get("error").Exists() && root.Get("error").Type != ojson.Null) {
		return "", fmt.Errorf("kilo_alpha_failed: search did not complete")
	}
	searched, explicitEmptySources := false, true
	var text, sources []string
	seen := map[string]bool{}
	addSource := func(value ojson.Result) {
		link := value.Get("url").String()
		if link == "" || seen[link] {
			return
		}
		seen[link] = true
		title := value.Get("title").String()
		if title == "" {
			sources = append(sources, link)
		} else {
			sources = append(sources, title+" — "+link)
		}
	}
	for _, item := range root.Get("output").Array() {
		if item.Get("encrypted_output").Exists() || item.Get("encrypted_content").Exists() {
			return "", fmt.Errorf("kilo_alpha_unsupported_state: opaque search state has no Alpha mapping")
		}
		if item.Get("type").String() == "web_search_call" {
			if item.Get("status").String() != "completed" || item.Get("action.type").String() != "search" {
				return "", fmt.Errorf("kilo_alpha_failed: search action did not complete")
			}
			searched = true
			rawSources := item.Get("action.sources")
			explicitEmptySources = explicitEmptySources && rawSources.IsArray() && len(rawSources.Array()) == 0
			for _, source := range rawSources.Array() {
				addSource(source)
			}
		}
		if item.Get("type").String() == "message" {
			for _, part := range item.Get("content").Array() {
				if part.Get("type").String() != "output_text" {
					continue
				}
				if value := part.Get("text").String(); value != "" {
					text = append(text, value)
				}
				for _, annotation := range part.Get("annotations").Array() {
					if annotation.Get("type").String() == "url_citation" {
						addSource(annotation)
					}
				}
			}
		}
	}
	if !searched {
		return "", fmt.Errorf("kilo_alpha_no_search: model did not execute web search")
	}
	if len(sources) == 0 {
		if explicitEmptySources && len(text) == 0 {
			return "Search completed with no results.", nil
		}
		return "", fmt.Errorf("kilo_alpha_missing_sources: search returned no usable source metadata")
	}
	return strings.TrimSpace(strings.Join(text, "\n\n") + "\n\nSources:\n" + strings.Join(sources, "\n")), nil
}
