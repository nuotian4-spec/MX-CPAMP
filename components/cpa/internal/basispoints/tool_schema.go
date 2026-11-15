package basispoints

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const toolSchemaURL = "https://basispoints.invalid/client-tool.json"

// Compile once per request, preserve json.Number, and never resolve external
// schemas. Validation errors must not echo private argument values.
func compileToolSchema(parameters object) (*jsonschema.Schema, error) {
	if parameters == nil {
		return nil, nil
	}
	raw, err := json.Marshal(parameters)
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("basispoints tool schema exceeds 1 MiB")
	}
	if err := validateToolNumberBudget(parameters); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(localSchemaLoader{})
	if err := compiler.AddResource(toolSchemaURL, parameters); err != nil {
		return nil, diagnoseToolSchema(parameters, err)
	}
	schema, err := compiler.Compile(toolSchemaURL)
	if err != nil {
		return nil, diagnoseToolSchema(parameters, err)
	}
	return schema, nil
}

// The schema library expands JSON exponents into big.Rat. Bound expansion
// before compilation and validation so tiny exponent strings cannot allocate
// unbounded integers. Ordinary large IDs remain exact.
func validateToolNumberBudget(value any) error {
	budget := 65536
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > 128 {
			return toolValidationResourceError{reason: "nesting_limit"}
		}
		switch v := value.(type) {
		case json.Number:
			raw := v.String()
			if len(raw) > 4096 {
				return toolValidationResourceError{reason: "number_precision_limit"}
			}
			digits := len(raw)
			if at := strings.IndexAny(raw, "eE"); at >= 0 {
				exponent, err := strconv.Atoi(raw[at+1:])
				if err != nil || exponent > 4096 || exponent < -4096 {
					return toolValidationResourceError{reason: "number_exponent_limit"}
				}
				if exponent < 0 {
					exponent = -exponent
				}
				digits += exponent
			}
			budget -= digits
			if budget < 0 {
				return toolValidationResourceError{reason: "number_budget_limit"}
			}
		case object:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value, 0)
}

type toolValidationResourceError struct{ reason string }

func (e toolValidationResourceError) Error() string {
	return "basispoints tool validation resource limit: " + e.reason
}

// BoundedToolValidationReason accepts only locally defined validation classes.
func BoundedToolValidationReason(reason string) string {
	switch reason {
	case "schema_mismatch", "nesting_limit", "number_precision_limit", "number_exponent_limit", "number_budget_limit":
		return reason
	default:
		return ""
	}
}

type toolArgumentsSchemaError struct{ tool, path, rule, reason string }

func (toolArgumentsSchemaError) Error() string {
	return "basispoints function arguments do not satisfy the declared tool schema or validation resource limits"
}
