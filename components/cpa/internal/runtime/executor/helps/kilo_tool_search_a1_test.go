package helps

import (
	"os"
	"reflect"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func kiloA1JSONValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, item := range current {
			if raw, ok := item.(string); ok && (key == "arguments" || key == "output") && ojson.ValidBytes([]byte(raw)) {
				item = ojson.ParseBytes([]byte(raw)).Value()
			}
			result[key] = kiloA1JSONValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(current))
		for _, item := range current {
			result = append(result, kiloA1JSONValue(item))
		}
		return result
	default:
		return value
	}
}

func TestKiloA1ToolSearchPythonDifferential(t *testing.T) {
	raw, err := os.ReadFile("testdata/kilo_a1_tool_search.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, vector := range ojson.ParseBytes(raw).Array() {
		t.Run(vector.Get("name").String(), func(t *testing.T) {
			c := &KiloToolSearch{}
			doc := ojson.NewDocString(vector.Get("request").Raw)
			err := c.Prepare(doc)
			if vector.Get("prepare_error").Bool() {
				if err == nil {
					t.Fatal("invalid request accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(kiloA1JSONValue(ojson.ParseBytes(doc.Bytes()).Value()), kiloA1JSONValue(vector.Get("expected_request").Value())) {
				t.Fatalf("request differs from original Python semantics: got=%s expected=%s", doc.String(), vector.Get("expected_request").Raw)
			}
			if c.Active != vector.Get("active").Bool() {
				t.Fatal("activation differs")
			}
			for i, event := range vector.Get("events").Array() {
				actual, err := c.Event([]byte(event.Raw))
				expected := vector.Get("expected_events").Array()[i]
				if expected.Get("error").Bool() {
					if err == nil {
						t.Fatal("invalid upstream arguments accepted")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if expected.Type == ojson.Null {
					if len(actual) != 0 {
						t.Fatal("function fragments exposed")
					}
					continue
				}
				if !reflect.DeepEqual(kiloA1JSONValue(ojson.ParseBytes(actual).Value()), kiloA1JSONValue(expected.Value())) {
					t.Fatalf("event differs: got=%s expected=%s", actual, expected.Raw)
				}
			}
		})
	}
}
