package basispoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func validationReasonArguments() map[string]object {
	var nested any = "PRIVATE_VALUE"
	for i := 0; i < 130; i++ {
		nested = []any{nested}
	}
	numbers := make([]any, 17)
	for i := range numbers {
		numbers[i] = json.Number("1e4096")
	}
	return map[string]object{
		"schema_mismatch":        {"n": "PRIVATE_VALUE"},
		"nesting_limit":          {"n": nested},
		"number_precision_limit": {"n": json.Number(strings.Repeat("9", 4097))},
		"number_exponent_limit":  {"n": json.Number("1e4097")},
		"number_budget_limit":    {"n": numbers},
	}
}

func TestToolValidationReasonAtomicAndBounded(t *testing.T) {
	for reason, args := range validationReasonArguments() {
		t.Run(reason, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{object{"type": "function", "name": "echo", "parameters": object{"type": "object", "properties": object{"n": object{"type": "number"}}, "required": []any{"n"}, "additionalProperties": false}}}
			_, bridge := mustPrepare(t, source, "reasons", new(ReplayCache))
			good := nativeCall(object{"name": "echo", "arguments": object{"n": json.Number("9007199254740993")}})
			bad := nativeCall(object{"name": "echo", "arguments": args})
			bad["id"], bad["call_id"] = "bad", "bad"
			response := object{"status": "completed", "output": []any{good, bad}}
			err := bridge.validateToolResponse(response)
			var classified toolArgumentsSchemaError
			if !errors.As(err, &classified) || classified.reason != reason {
				t.Fatalf("classification = %v, want %s", err, reason)
			}
			failure := bridge.ToolValidationFailure(response, err)
			if failure.ValidationReason != reason || failure.Reason != "schema_validation" || failure.SchemaTool != "echo" {
				t.Fatalf("failure = %+v", failure)
			}
			raw := []byte(quoted(object{"type": "response.failed", "response": object{"error": failure.Details()}}))
			parsed := ParseBridgeFailure(raw)
			if parsed == nil || parsed.ValidationReason != reason || parsed.SchemaTool != "echo" {
				t.Fatalf("round trip = %+v", parsed)
			}
			if bytes.Contains(raw, []byte("PRIVATE")) || len(parsed.SchemaPath) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			if reason != "schema_mismatch" && (parsed.SchemaPath != "$" || parsed.SchemaRule != "") {
				t.Fatal("resource diagnostic guessed a schema rule")
			}
			wire := sse(object{"type": "response.output_item.added", "item": good, "output_index": 0}) + sse(object{"type": "response.completed", "response": response})
			stream := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
			output, readErr := io.ReadAll(stream)
			_ = stream.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			calls, failures := 0, 0
			if err := readEvents(bytes.NewReader(output), func(_ string, data []byte) error {
				var event object
				if err := decode(data, &event); err != nil {
					return err
				}
				if text(event["type"]) == "response.output_item.done" || text(event["type"]) == "response.output_item.added" {
					calls++
				}
				if text(event["type"]) == "response.failed" {
					failures++
					if f := ParseBridgeFailure(data); f == nil || f.ValidationReason != reason {
						t.Fatal("stream lost typed reason")
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || failures != 1 || bridge.replay.get("reasons", "call_native") != nil {
				t.Fatalf("partial publication: calls=%d failed=%d", calls, failures)
			}
		})
	}
}

func TestToolResourceBudgetTypedAndExact(t *testing.T) {
	for reason, args := range validationReasonArguments() {
		if reason == "schema_mismatch" {
			continue
		}
		err := validateToolNumberBudget(args)
		var resource toolValidationResourceError
		if !errors.As(err, &resource) || resource.reason != reason {
			t.Fatalf("resource=%v want=%s", err, reason)
		}
	}
	for _, number := range []string{"9007199254740993", strings.Repeat("9", 4096), "1e4096", "1e-4096"} {
		if err := validateToolNumberBudget(object{"n": json.Number(number)}); err != nil {
			t.Fatal(err)
		}
	}
	if BoundedToolValidationReason("PRIVATE_REASON") != "" {
		t.Fatal("unknown reason accepted")
	}
}

func TestFunctionCodeResourceReasonPreserved(t *testing.T) {
	source := testSource()
	source["tools"] = []any{functionCodeBranchTool("oneOf")}
	_, bridge := mustPrepare(t, source, "scope", nil)
	metadata := quoted(object{"completion_summary": "PRIVATE_VALUE", "n": json.Number("1e4097")})
	_, err := bridge.translateCall(functionCodeTestNative(t, "branch_tool", "", metadata))
	var classified toolArgumentsSchemaError
	if !errors.As(err, &classified) || classified.reason != "number_exponent_limit" || classified.tool != "branch_tool" {
		t.Fatalf("classification=%+v err=%v", classified, err)
	}
}
