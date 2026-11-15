package helps

import (
	"reflect"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloA2AlphaPlan(t *testing.T) {
	raw := `{"id":"synthetic-parent","model":"gpt-5.4","input":[{"role":"assistant","content":[{"type":"output_text","text":"prior context"}]},{"type":"reasoning","encrypted_content":"opaque"}],"commands":{"search_query":[{"q":"synthetic query","domains":["docs.example.com"]}],"response_length":"short"},"settings":{"filters":{"allowed_domains":["example.com"]}}}`
	p, err := ParseKiloAlphaSearch([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Request(0)
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxOutputTokens != 2500 || doc.Get("tools.0.external_web_access").Bool() || !reflect.DeepEqual(p.Queries[0].Domains, []string{"docs.example.com"}) {
		t.Fatal(doc.String())
	}
	for i, item := range ojson.Get(raw, "input").Array() {
		if !reflect.DeepEqual(item.Value(), doc.Get("input").Array()[i].Value()) {
			t.Fatal("input history changed")
		}
	}
	if doc.Get("input.2.content").String() != "synthetic query" {
		t.Fatal("query changed")
	}
	if doc.Get("max_output_tokens").Int() != 2500 {
		t.Fatal("short budget changed")
	}
}

func TestKiloA2AlphaBatchPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, patch string
		valid       bool
	}{
		{"cached", `{}`, true},
		{"live", `{"settings":{"external_web_access":true}}`, true},
		{"indexed", `{"settings":{"external_web_access":"indexed"}}`, false},
		{"recency", `{"commands":{"search_query":[{"q":"first"},{"q":"second","recency":7}]}}`, false},
		{"mixed", `{"commands":{"search_query":[{"q":"first"}],"open":[{"ref":"synthetic"}]}}`, false},
		{"empty_intersection", `{"settings":{"filters":{"allowed_domains":["one.invalid"]}},"commands":{"search_query":[{"q":"first","domains":["two.invalid"]}]}}`, false},
		{"excluded_all", `{"settings":{"filters":{"allowed_domains":["example.com"],"blocked_domains":["example.com"]}}}`, false},
		{"excluded_subdomain", `{"settings":{"filters":{"allowed_domains":["example.com"],"blocked_domains":["private.example.com"]}}}`, false},
		{"excluded_disjoint", `{"settings":{"filters":{"allowed_domains":["example.com","other.invalid"],"blocked_domains":["example.com"]}}}`, true},
		{"budget", `{"max_output_tokens":2000}`, true},
		{"fractional_budget", `{"max_output_tokens":2.5}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := ojson.NewDocString(`{"id":"synthetic","model":"gpt-5.4","commands":{"search_query":[{"q":"first"}]}}`)
			for k, v := range ojson.Parse(tc.patch).Map() {
				_ = doc.Set(k, v.Value())
			}
			p, err := ParseKiloAlphaSearch(doc.Bytes())
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if !tc.valid && p != nil {
				t.Fatal("partially valid batch escaped preflight")
			}
			if tc.name == "live" && !p.Queries[0].ExternalWebAccess {
				t.Fatal("live mode lost")
			}
		})
	}
}
