package helps

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestKiloA2FinalAlphaBarrier(t *testing.T) {
	p, err := ParseKiloAlphaSearch([]byte(`{"id":"synthetic","model":"gpt-6.1-sol","commands":{"search_query":[{"q":"query","domains":["example.com"]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithKiloAlphaContract(context.Background(), p.Queries[0], p.MaxOutputTokens)
	for _, field := range []string{"", "model", "tools", "tools.0.external_web_access", "tools.0.filters.allowed_domains", "max_output_tokens", "include", "tool_choice"} {
		t.Run(field, func(t *testing.T) {
			doc, _ := p.Request(0)
			_ = doc.Set("model", "openai/gpt-6.1-sol")
			if field != "" {
				_ = doc.Delete(field)
			}
			before := doc.String()
			err := ValidateKiloAlphaPayload(ctx, doc, "gpt-6.1-sol")
			if (err == nil) != (field == "") || doc.String() != before {
				t.Fatalf("barrier err=%v mutated=%v", err, doc.String() != before)
			}
		})
	}
}

func TestKiloA2AlphaResultKinds(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"sources", `[{"type":"web_search_call","status":"completed","action":{"type":"search","sources":[{"url":"https://example.com","title":"Synthetic"}]}},{"type":"message","content":[{"type":"output_text","text":"Synthetic result"}]}]`, true},
		{"empty", `[{"type":"web_search_call","status":"completed","action":{"type":"search","sources":[]}}]`, true},
		{"no_search", `[{"type":"message","content":[{"type":"output_text","text":"ordinary answer"}]}]`, false},
		{"missing_sources", `[{"type":"web_search_call","status":"completed","action":{"type":"search"}},{"type":"message","content":[{"type":"output_text","text":"answer"}]}]`, false},
		{"failed", `[{"type":"web_search_call","status":"failed","action":{"type":"search"}}]`, false},
		{"opaque", `[{"type":"web_search_call","status":"completed","encrypted_output":"opaque","action":{"type":"search","sources":[]}}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := ojson.NewDocString(`{"status":"completed"}`)
			_ = doc.SetRaw("output", tc.output)
			output, err := KiloAlphaResult(doc.Bytes())
			if (err == nil) != tc.valid || (tc.valid && output == "") {
				t.Fatalf("output=%q err=%v", output, err)
			}
		})
	}
}

func TestKiloA2AlphaNonEmptySourcesWithoutURL(t *testing.T) {
	output, err := KiloAlphaResult([]byte(`{"status":"completed","output":[{"type":"web_search_call","status":"completed","action":{"type":"search","sources":[{"title":"source without URL"}]}}]}`))
	if err == nil || err.Error() != "kilo_alpha_missing_sources: search returned no usable source metadata" || output != "" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}
