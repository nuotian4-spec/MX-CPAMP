package basispoints

import (
	"errors"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Retain only schema-declared property names and a validator keyword, never values.
func argumentSchemaFailure(info tool, failure error) error {
	result := toolArgumentsSchemaError{tool: safeSchemaName(info.Name), path: "$", reason: "schema_mismatch"}
	var resource toolValidationResourceError
	if errors.As(failure, &resource) {
		result.reason = resource.reason
		return result
	}
	var validation *jsonschema.ValidationError
	if !errors.As(failure, &validation) {
		return result
	}
	for depth := 0; len(validation.Causes) > 0 && depth < 32; depth++ {
		validation = validation.Causes[0]
	}
	path := []string{}
	schema := info.Parameters
	for _, part := range validation.InstanceLocation {
		properties, _ := schema["properties"].(object)
		if child, ok := properties[part].(object); ok {
			path = append(path, part)
			schema = child
		} else if _, err := strconv.ParseUint(part, 10, 32); err == nil && schema["items"] != nil {
			path = append(path, part)
			schema, _ = schema["items"].(object)
		} else {
			break
		}
	}
	result.path = "$" + schemaPointer(path, true)
	if len(result.path) > 256 {
		result.path = "$"
	}
	if validation.ErrorKind != nil {
		keywords := validation.ErrorKind.KeywordPath()
		if len(keywords) > 0 {
			result.rule = boundedArgumentRule(keywords[len(keywords)-1])
		}
	}
	return result
}

func boundedArgumentRule(rule string) string {
	switch rule {
	case "type", "required", "additionalProperties", "unevaluatedProperties", "minLength", "maxLength", "pattern", "format", "enum", "const", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minItems", "maxItems", "uniqueItems", "contains", "minContains", "maxContains", "minProperties", "maxProperties", "allOf", "anyOf", "oneOf", "not", "falseSchema":
		return rule
	}
	return "schema"
}

func boundedArgumentPath(path string) string {
	if len(path) > 256 || !strings.HasPrefix(path, "$") {
		return "$"
	}
	return safeSchemaName(path)
}
