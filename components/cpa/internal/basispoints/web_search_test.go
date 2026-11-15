package basispoints

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestBPSWebSearchNeverPublishesInternalCatalog(t *testing.T) {
	cache := &CatalogCache{}
	wire, bridge, err := PrepareWithCatalog([]byte(`{"model":"m","input":"hello","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`), "search-scope", nil, cache)
	if err != nil {
		t.Fatal(err)
	}
	doc := ojson.NewDoc(wire)
	if err = bridge.EnableWebSearch(doc, false); err != nil {
		t.Fatal(err)
	}
	_, next, err := PrepareWithCatalog([]byte(`{"model":"m","input":"hello"}`), "search-scope", nil, cache)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := next.tools[WebSearchFunction]; found {
		t.Fatal("internal tool entered client catalog cache")
	}
	for _, kind := range []string{"computer_call", "code_interpreter_call", "web_search_call"} {
		call := nativeCall(object{"name": WebSearchFunction, "arguments": object{"query": "q"}})
		_, err = bridge.WebSearchCalls(object{"status": "completed", "output": []any{call, object{"type": kind}}})
		if err == nil {
			t.Fatalf("unknown mixed output %s accepted", kind)
		}
	}
}

func TestBPSArgumentDiagnosticsAreBoundedSchemaMetadata(t *testing.T) {
	_, bridge, err := Prepare([]byte(`{"model":"m","input":"hi","tools":[{"type":"function","name":"client_echo","parameters":{"type":"object","properties":{"value":{"type":"string","minLength":5}},"required":["value"],"additionalProperties":false}}]}`), "diagnostic", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := object{"status": "completed", "output": []any{nativeCall(object{"name": "client_echo", "arguments": object{"value": "x"}})}}
	err = bridge.validateToolResponse(response)
	if err == nil {
		t.Fatal("invalid schema accepted")
	}
	failure := bridge.ToolValidationFailure(response, err)
	if failure.SchemaTool != "client_echo" || failure.SchemaPath != "$/value" || failure.SchemaRule != "minLength" {
		t.Fatalf("metadata=%+v", failure)
	}
	response["output"] = []any{nativeCall(object{"name": "client_echo", "arguments": object{"value": "valid", "PRIVATE_ARGUMENT_VALUE": "PRIVATE_SECRET_VALUE"}})}
	err = bridge.validateToolResponse(response)
	failure = bridge.ToolValidationFailure(response, err)
	if strings.Contains(quoted(failure.Details()), "PRIVATE_") {
		t.Fatalf("argument contents leaked: %+v", failure)
	}
}
