package basispoints

import (
	"reflect"
	"testing"
)

func TestDirectCatalogNamespaceIdentity(t *testing.T) {
	for _, kind := range []string{"function", "custom"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				label, name      string
				namespace        any
				wantName, wantNS string
				reject           bool
			}{
				{label: "split", name: "echo", namespace: "a", wantName: "echo", wantNS: "a"},
				{label: "redundant", name: "a.echo", namespace: "a", wantName: "echo", wantNS: "a"},
				{label: "qualified", name: "a.echo", wantName: "echo", wantNS: "a"},
				{label: "display prefix", name: "functions.a.echo", wantName: "echo", wantNS: "a"},
				{label: "bare no guessing", name: "echo", reject: true},
				{label: "conflict", name: "b.echo", namespace: "a", reject: true},
				{label: "unknown namespace", name: "a.echo", namespace: "missing", reject: true},
				{label: "invalid namespace", name: "a.echo", namespace: 1, reject: true},
				{label: "exact functions name", name: "functions.foo", wantName: "functions.foo"},
				{label: "dotted local name", name: "local.dot", namespace: "a", wantName: "local.dot", wantNS: "a"},
				{label: "ambiguous metadata", name: "a.echo", namespace: "a", reject: true},
			} {
				t.Run(tc.label, func(t *testing.T) {
					definition := func(name string) object {
						return object{"type": kind, "name": name, "parameters": object{"type": "object"}}
					}
					nested := []any{definition("echo"), definition("local.dot")}
					if tc.label == "ambiguous metadata" {
						nested = append(nested, definition("a.echo"))
					}
					source := testSource()
					source["tools"] = []any{
						object{"type": "namespace", "name": "a", "tools": nested},
						object{"type": "namespace", "name": "b", "tools": []any{definition("echo")}},
						definition("functions.foo"), definition("foo"),
					}
					_, bridge := mustPrepare(t, source, "scope", nil)
					native := object{"type": "function_call", "name": tc.name, "call_id": "call_identity", "arguments": `{"s":"原值","n":9007199254740993}`}
					if tc.namespace != nil {
						native["namespace"] = tc.namespace
					}
					if kind == "custom" {
						native["type"], native["input"] = "custom_tool_call", "line 1\n原值\n"
						delete(native, "arguments")
					}
					before := make(object, len(native))
					for key, value := range native {
						before[key] = value
					}
					call, err := bridge.translateCall(native)
					if tc.reject {
						if err == nil {
							t.Fatalf("expected rejection, got %+v", call)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if call["name"] != tc.wantName || text(call["namespace"]) != tc.wantNS || call["call_id"] != native["call_id"] {
						t.Fatalf("identity changed: %+v", call)
					}
					if !reflect.DeepEqual(before, native) {
						t.Fatalf("native mutated: %+v", native)
					}
					if kind == "custom" && call["input"] != native["input"] {
						t.Fatal("custom input changed")
					}
					if kind == "function" {
						var got, want object
						_ = decode([]byte(text(call["arguments"])), &got)
						_ = decode([]byte(text(native["arguments"])), &want)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("arguments changed: %+v", got)
						}
					}
				})
			}
		})
	}
}
