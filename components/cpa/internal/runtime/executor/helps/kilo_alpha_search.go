package helps

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// KiloAlphaQuery is one independently filtered keyword search.
type KiloAlphaQuery struct {
	Text              string
	Domains           []string
	ExternalWebAccess bool
}

// KiloAlphaPlan is fully validated before the first query is executed.
type KiloAlphaPlan struct {
	ID, Model        string
	Input, Reasoning ojson.Result
	Queries          []KiloAlphaQuery
	MaxOutputTokens  int64
}

// KiloAlphaError identifies an unsupported or malformed Alpha contract.
type KiloAlphaError struct{ Code, Message string }

func (e *KiloAlphaError) Error() string         { return e.Message }
func (e *KiloAlphaError) ErrorCode() string     { return e.Code }
func (e *KiloAlphaError) StatusCode() int       { return 400 }
func (e *KiloAlphaError) IsRequestScoped() bool { return true }

func kiloAlphaInvalid(code, message string) error {
	return &KiloAlphaError{Code: code, Message: message}
}

func kiloAlphaFields(value ojson.Result, allowed ...string) error {
	if !value.IsObject() {
		return kiloAlphaInvalid("kilo_alpha_invalid_request", "Alpha fields must be objects")
	}
	for key := range value.Map() {
		found := false
		for _, name := range allowed {
			found = found || key == name
		}
		if !found {
			return kiloAlphaInvalid("kilo_alpha_unsupported_field", "Unsupported Alpha field: "+key)
		}
	}
	return nil
}

// ParseKiloAlphaSearch rejects the entire batch if any item lacks an exact mapping.
func ParseKiloAlphaSearch(raw []byte) (*KiloAlphaPlan, error) {
	if !ojson.ValidBytes(raw) {
		return nil, kiloAlphaInvalid("kilo_alpha_invalid_request", "Invalid Alpha JSON")
	}
	root := ojson.ParseBytes(raw)
	if err := kiloAlphaFields(root, "id", "model", "input", "reasoning", "commands", "settings", "max_output_tokens"); err != nil {
		return nil, err
	}
	p := &KiloAlphaPlan{ID: root.Get("id").String(), Model: root.Get("model").String(), Input: root.Get("input"), Reasoning: root.Get("reasoning"), MaxOutputTokens: 2500}
	if root.Get("id").Type != ojson.String || root.Get("model").Type != ojson.String || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Model) == "" {
		return nil, kiloAlphaInvalid("kilo_alpha_invalid_request", "Alpha id and model are required strings")
	}
	if p.Input.Exists() && p.Input.Type != ojson.String && !p.Input.IsArray() {
		return nil, kiloAlphaInvalid("kilo_alpha_unsupported_input", "Alpha input must be text or response items")
	}
	if p.Reasoning.Exists() && !p.Reasoning.IsObject() {
		return nil, kiloAlphaInvalid("kilo_alpha_invalid_request", "Alpha reasoning must be an object")
	}
	budget := root.Get("max_output_tokens")
	if budget.Exists() {
		if budget.Type != ojson.Number || budget.Int() <= 0 || budget.Float() != float64(budget.Int()) {
			return nil, kiloAlphaInvalid("kilo_alpha_unsupported_budget", "Alpha max_output_tokens must be a positive integer")
		}
		p.MaxOutputTokens = budget.Int()
	}
	commands := root.Get("commands")
	if err := kiloAlphaFields(commands, "search_query", "response_length"); err != nil {
		return nil, err
	}
	if length := commands.Get("response_length"); length.Exists() && (length.Type != ojson.String || length.String() != "short") {
		return nil, kiloAlphaInvalid("kilo_alpha_unsupported_budget", "Only the verified short Alpha response length is supported")
	}
	settings := root.Get("settings")
	live := false
	var allow, block []string
	if settings.Exists() {
		if err := kiloAlphaFields(settings, "allowed_callers", "external_web_access", "filters"); err != nil {
			return nil, err
		}
		if callers := settings.Get("allowed_callers"); callers.Exists() {
			if !callers.IsArray() || len(callers.Array()) != 1 || callers.Array()[0].Type != ojson.String || callers.Array()[0].String() != "direct" {
				return nil, kiloAlphaInvalid("kilo_alpha_unsupported_callers", "Only direct Alpha callers are supported")
			}
		}
		if access := settings.Get("external_web_access"); access.Exists() {
			if access.Type != ojson.True && access.Type != ojson.False {
				return nil, kiloAlphaInvalid("kilo_alpha_unsupported_mode", "Alpha external_web_access must be boolean")
			}
			live = access.Bool()
		}
		if filters := settings.Get("filters"); filters.Exists() {
			if err := kiloAlphaFields(filters, "allowed_domains", "blocked_domains"); err != nil {
				return nil, err
			}
			var err error
			allow, err = kiloAlphaDomains(filters.Get("allowed_domains"))
			if err != nil {
				return nil, err
			}
			block, err = kiloAlphaDomains(filters.Get("blocked_domains"))
			if err != nil {
				return nil, err
			}
		}
	}
	queries := commands.Get("search_query")
	if !queries.IsArray() || len(queries.Array()) == 0 {
		return nil, kiloAlphaInvalid("kilo_alpha_invalid_request", "Alpha search_query must be a nonempty array")
	}
	for _, value := range queries.Array() {
		if value.Get("recency").Exists() {
			return nil, kiloAlphaInvalid("kilo_alpha_unsupported_recency", "Kilo Alpha has no guaranteed recency filter")
		}
		if err := kiloAlphaFields(value, "q", "domains"); err != nil {
			return nil, err
		}
		if value.Get("q").Type != ojson.String || strings.TrimSpace(value.Get("q").String()) == "" {
			return nil, kiloAlphaInvalid("kilo_alpha_invalid_request", "Alpha query must be a nonempty string")
		}
		domains, err := kiloAlphaDomains(value.Get("domains"))
		if err != nil {
			return nil, err
		}
		domains, err = kiloAlphaDomainIntersection(allow, domains, block)
		if err != nil {
			return nil, err
		}
		p.Queries = append(p.Queries, KiloAlphaQuery{Text: value.Get("q").String(), Domains: domains, ExternalWebAccess: live})
	}
	return p, nil
}

func kiloAlphaDomains(value ojson.Result) ([]string, error) {
	if !value.Exists() {
		return nil, nil
	}
	if !value.IsArray() || len(value.Array()) == 0 {
		return nil, kiloAlphaInvalid("kilo_alpha_invalid_domains", "Domain filters must be nonempty arrays")
	}
	result := make([]string, 0, len(value.Array()))
	for _, item := range value.Array() {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item.String()), "."))
		parsed, err := url.Parse("https://" + domain)
		if item.Type != ojson.String || domain == "" || err != nil || parsed.Hostname() != domain || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || strings.ContainsAny(domain, "* \\:/@") {
			return nil, kiloAlphaInvalid("kilo_alpha_invalid_domains", "Domain filters require hostnames without paths or wildcards")
		}
		result = append(result, domain)
	}
	return result, nil
}

func kiloAlphaWithin(domain, parent string) bool {
	return domain == parent || strings.HasSuffix(domain, "."+parent)
}

func kiloAlphaDomainIntersection(global, local, blocked []string) ([]string, error) {
	allowed := append([]string(nil), global...)
	if len(allowed) == 0 {
		allowed = append(allowed, local...)
	} else if len(local) > 0 {
		allowed = nil
		for _, a := range global {
			for _, b := range local {
				if kiloAlphaWithin(a, b) {
					allowed = append(allowed, a)
				} else if kiloAlphaWithin(b, a) {
					allowed = append(allowed, b)
				}
			}
		}
		if len(allowed) == 0 {
			return nil, kiloAlphaInvalid("kilo_alpha_empty_domains", "Alpha allowed-domain intersection is empty")
		}
	}
	if len(blocked) > 0 && len(allowed) == 0 {
		return nil, kiloAlphaInvalid("kilo_alpha_unsupported_domains", "Blocked domains require a representable allowlist")
	}
	result := make([]string, 0, len(allowed))
	for _, domain := range allowed {
		excluded := false
		for _, deny := range blocked {
			if kiloAlphaWithin(domain, deny) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		for _, deny := range blocked {
			if kiloAlphaWithin(deny, domain) {
				return nil, kiloAlphaInvalid("kilo_alpha_unsupported_domains", "A blocked subdomain cannot be represented by the upstream allowlist")
			}
		}
		result = append(result, domain)
	}
	if len(allowed) > 0 && len(result) == 0 {
		return nil, kiloAlphaInvalid("kilo_alpha_empty_domains", "Alpha domain filters exclude every allowed domain")
	}
	return result, nil
}

// Request preserves every input item and adds only the selected keyword query.
func (p *KiloAlphaPlan) Request(index int) (*ojson.Doc, error) {
	if index < 0 || index >= len(p.Queries) {
		return nil, fmt.Errorf("Alpha query index out of range")
	}
	q := p.Queries[index]
	input := make([]any, 0)
	if p.Input.Type == ojson.String {
		input = append(input, map[string]any{"role": "user", "content": p.Input.String()})
	} else {
		for _, item := range p.Input.Array() {
			input = append(input, item.Value())
		}
	}
	input = append(input, map[string]any{"role": "user", "content": q.Text})
	doc := ojson.NewDocString(`{}`)
	_ = doc.Set("model", p.Model)
	_ = doc.Set("input", input)
	_ = doc.Set("max_output_tokens", p.MaxOutputTokens)
	_ = doc.Set("include", []string{"web_search_call.action.sources"})
	tool := map[string]any{"type": "web_search", "external_web_access": q.ExternalWebAccess}
	if len(q.Domains) > 0 {
		tool["filters"] = map[string]any{"allowed_domains": q.Domains}
	}
	_ = doc.Set("tools", []any{tool})
	_ = doc.Set("tool_choice", map[string]any{"type": "web_search"})
	if p.Reasoning.Exists() {
		_ = doc.Set("reasoning", p.Reasoning.Value())
	}
	return doc, nil
}
