package helps

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type BPSAlphaSearchPlan struct {
	Queries  []string
	Sessions []*BPSSearchSession
	Requests [][]byte
}

// BPSAlphaSearchMode preserves both forms used by the typed Codex contract.
func BPSAlphaSearchMode(body []byte) (string, error) {
	mode := ojson.GetBytes(body, "settings.external_web_access")
	if !alphaValueSet(mode) || mode.Raw == "true" || mode.String() == "live" {
		return "live", nil
	}
	if mode.Raw == "false" || mode.String() == "cached" {
		return "cached", nil
	}
	if mode.String() == "indexed" {
		return "indexed", nil
	}
	return "", errors.New("unsupported Alpha Search external_web_access mode")
}

func PrepareBPSAlphaSearch(body []byte) (*BPSAlphaSearchPlan, error) {
	if !ojson.ValidBytes(body) {
		return nil, errors.New("Alpha Search request must be valid JSON")
	}
	root := ojson.ParseBytes(body)
	for _, key := range []string{"id", "model"} {
		if value := root.Get(key); value.Type != ojson.String || strings.TrimSpace(value.String()) == "" {
			return nil, fmt.Errorf("Alpha Search %s must be a non-empty string", key)
		}
	}
	mode, errMode := BPSAlphaSearchMode(body)
	if errMode != nil {
		return nil, errMode
	}
	if mode != "live" {
		return nil, errors.New("Alpha Search cached and indexed modes require a native Alpha Search backend")
	}
	if alphaValueSet(root.Get("reasoning")) {
		return nil, errors.New("Alpha Search reasoning is unsupported by the configured web search backend")
	}
	settings := root.Get("settings")
	if alphaValueSet(settings) && !settings.IsObject() {
		return nil, errors.New("Alpha Search settings must be an object")
	}
	for key, value := range settings.Map() {
		if !alphaValueSet(value) {
			continue
		}
		switch key {
		case "external_web_access", "filters":
		case "allowed_callers":
			if !value.IsArray() || len(value.Array()) == 0 {
				return nil, errors.New("Alpha Search requires the direct caller")
			}
			for _, caller := range value.Array() {
				if caller.Type != ojson.String || caller.String() != "direct" {
					return nil, errors.New("Alpha Search supports only the direct caller")
				}
			}
		default:
			return nil, fmt.Errorf("Alpha Search setting %s is unsupported by the configured web search backend", key)
		}
	}
	filters := settings.Get("filters")
	if alphaValueSet(filters) && !filters.IsObject() {
		return nil, errors.New("Alpha Search filters must be an object")
	}
	for key, value := range filters.Map() {
		if key != "allowed_domains" && alphaValueSet(value) {
			return nil, fmt.Errorf("Alpha Search filter %s is unsupported by the configured web search backend", key)
		}
	}
	globalDomains, errDomains := alphaDomains(filters.Get("allowed_domains"))
	if errDomains != nil {
		return nil, errDomains
	}
	commands := root.Get("commands")
	if alphaValueSet(commands) && !commands.IsObject() {
		return nil, errors.New("Alpha Search commands must be an object")
	}
	for key, value := range commands.Map() {
		if key != "search_query" && key != "response_length" && alphaValueSet(value) {
			return nil, fmt.Errorf("Alpha Search command %s is unsupported by the configured web search backend", key)
		}
	}
	maxTokens := uint64(4096)
	if length := commands.Get("response_length"); alphaValueSet(length) {
		switch length.String() {
		case "short":
			maxTokens = 1024
		case "medium":
			maxTokens = 2048
		case "long":
		default:
			return nil, errors.New("Alpha Search response_length must be short, medium or long")
		}
	}
	if limit := root.Get("max_output_tokens"); alphaValueSet(limit) {
		parsed, errParse := strconv.ParseUint(limit.Raw, 10, 64)
		if errParse != nil || parsed == 0 {
			return nil, errors.New("Alpha Search max_output_tokens must be a positive integer")
		}
		if parsed < maxTokens {
			maxTokens = parsed
		}
	}
	contextMessages, errInput := alphaInputMessages(root.Get("input"))
	if errInput != nil {
		return nil, errInput
	}
	queries := commands.Get("search_query")
	if alphaValueSet(queries) && !queries.IsArray() {
		return nil, errors.New("Alpha Search search_query must be an array")
	}
	plan := &BPSAlphaSearchPlan{}
	for _, query := range queries.Array() {
		for key, value := range query.Map() {
			if key != "q" && key != "domains" && alphaValueSet(value) {
				return nil, fmt.Errorf("Alpha Search query field %s is unsupported by the configured web search backend", key)
			}
		}
		text := query.Get("q")
		if text.Type != ojson.String || strings.TrimSpace(text.String()) == "" {
			return nil, errors.New("Alpha Search query q must be a non-empty string")
		}
		domains, errQueryDomains := alphaDomains(query.Get("domains"))
		if errQueryDomains != nil {
			return nil, errQueryDomains
		}
		domains, errIntersect := alphaIntersectDomains(globalDomains, domains)
		if errIntersect != nil {
			return nil, errIntersect
		}
		plan.Queries = append(plan.Queries, text.String())
		plan.Sessions = append(plan.Sessions, &BPSSearchSession{Domains: domains})
	}
	if len(plan.Queries) == 0 {
		input := root.Get("input")
		if input.Type != ojson.String || strings.TrimSpace(input.String()) == "" {
			return nil, errors.New("Alpha Search requires search_query commands or explicit text input")
		}
		plan.Queries = []string{input.String()}
		plan.Sessions = []*BPSSearchSession{{Domains: globalDomains}}
		contextMessages = nil
	}
	if len(plan.Queries) > 4 || maxTokens < uint64(len(plan.Queries)) {
		return nil, errors.New("Alpha Search supports at most four queries within the output token budget")
	}
	for i, query := range plan.Queries {
		doc := ojson.NewDoc(plan.Sessions[i].Request(query))
		_ = doc.Set("max_tokens", maxTokens/uint64(len(plan.Queries)))
		messages := append([]any(nil), contextMessages...)
		messages = append(messages, map[string]any{"role": "user", "content": "Perform a web search for the query: " + query + "\nUse source URLs in the answer; do not invent reference identifiers."})
		_ = doc.Set("messages", messages)
		plan.Requests = append(plan.Requests, doc.Bytes())
	}
	return plan, nil
}

func (p *BPSAlphaSearchPlan) Output(index int, raw []byte) (string, error) {
	parsed, sources, errParse := p.Sessions[index].ParseResult(raw, p.Queries[index])
	if errParse != nil {
		return "", errParse
	}
	if len(sources) == 0 {
		return "Query: " + p.Queries[index] + "\nNo search results found.", nil
	}
	var output strings.Builder
	output.WriteString("Query: " + p.Queries[index] + "\n")
	output.WriteString(ojson.GetBytes(parsed, "text").String())
	output.WriteString("\nSources:\n")
	for _, source := range sources {
		fmt.Fprintf(&output, "%s — %s\n", source["title"], source["url"])
	}
	return output.String(), nil
}

func alphaValueSet(value ojson.Result) bool {
	return value.Exists() && value.Raw != "null"
}

func alphaDomains(value ojson.Result) ([]string, error) {
	if !alphaValueSet(value) {
		return nil, nil
	}
	if !value.IsArray() {
		return nil, errors.New("Alpha Search domains must be an array")
	}
	var domains []string
	for _, item := range value.Array() {
		domain := strings.ToLower(strings.TrimSpace(item.String()))
		if item.Type != ojson.String || domain == "" || strings.ContainsAny(domain, "/:@*?# \\ \t\r\n") {
			return nil, errors.New("Alpha Search domains must contain domain names")
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func alphaIntersectDomains(a, b []string) ([]string, error) {
	if len(a) == 0 {
		return b, nil
	}
	if len(b) == 0 {
		return a, nil
	}
	var result []string
	for _, left := range a {
		for _, right := range b {
			if left == right || strings.HasSuffix(left, "."+right) {
				result = append(result, left)
			} else if strings.HasSuffix(right, "."+left) {
				result = append(result, right)
			}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Alpha Search domain filters have no searchable intersection")
	}
	return result, nil
}

func alphaInputMessages(input ojson.Result) ([]any, error) {
	if !alphaValueSet(input) {
		return nil, nil
	}
	if input.Type == ojson.String {
		return []any{map[string]any{"role": "user", "content": input.String()}}, nil
	}
	if !input.IsArray() {
		return nil, errors.New("Alpha Search input must be text or text messages")
	}
	var messages []any
	for _, item := range input.Array() {
		role := item.Get("role").String()
		if item.Get("type").String() != "message" || (role != "user" && role != "assistant") {
			return nil, errors.New("Alpha Search input item is unsupported by the configured web search backend")
		}
		var texts []string
		if !item.Get("content").IsArray() {
			return nil, errors.New("Alpha Search message content must be a text array")
		}
		for _, part := range item.Get("content").Array() {
			kind := part.Get("type").String()
			if (kind != "input_text" && kind != "output_text") || part.Get("text").Type != ojson.String {
				return nil, errors.New("Alpha Search non-text history is unsupported by the configured web search backend")
			}
			texts = append(texts, part.Get("text").String())
		}
		messages = append(messages, map[string]any{"role": role, "content": strings.Join(texts, "\n")})
	}
	return messages, nil
}
