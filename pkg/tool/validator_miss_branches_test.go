package tool

import (
	"strings"
	"testing"
)

func TestValidatorNilSchemaAndNilParams(t *testing.T) {
	t.Parallel()

	v := DefaultValidator{}
	if err := v.Validate(nil, nil); err != nil {
		t.Fatalf("expected nil error for nil schema, got %v", err)
	}

	schema := &JSONSchema{Type: "object"}
	if err := v.Validate(nil, schema); err != nil {
		t.Fatalf("expected nil error for nil params, got %v", err)
	}
}

func TestValidatorValidateValue_NilSchema(t *testing.T) {
	t.Parallel()

	v := DefaultValidator{}
	if err := v.validateValue("x", nil, "field"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidatorInferredTypesAndErrorBranches(t *testing.T) {
	t.Parallel()

	v := DefaultValidator{}

	if err := v.validateValue([]any{"a"}, &JSONSchema{Items: &JSONSchema{Type: "string"}}, ""); err != nil {
		t.Fatalf("expected inferred array validation success, got %v", err)
	}
	if err := v.validateValue(map[string]any{}, &JSONSchema{Properties: map[string]any{"x": map[string]any{"type": "string"}}}, ""); err != nil {
		t.Fatalf("expected inferred object validation success, got %v", err)
	}

	err := v.validateValue(123, &JSONSchema{Pattern: "^a$"}, "p")
	if err == nil || !strings.Contains(err.Error(), "expected string") {
		t.Fatalf("expected pattern type error, got %v", err)
	}

	err = v.validateValue("x", &JSONSchema{Pattern: "["}, "p")
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("expected invalid pattern error, got %v", err)
	}

	min := 1.0
	err = v.validateValue("x", &JSONSchema{Minimum: &min}, "n")
	if err == nil || !strings.Contains(err.Error(), "expected number") {
		t.Fatalf("expected minimum type error, got %v", err)
	}

	err = v.validateValue("x", &JSONSchema{Type: "object"}, "o")
	if err == nil || !strings.Contains(err.Error(), "expected object") {
		t.Fatalf("expected object type error, got %v", err)
	}
}

func TestValidatorSchemaFromMap_MinMaxAndIntegerFloat32(t *testing.T) {
	t.Parallel()

	min := 1.0
	max := 2.0
	s := schemaFromMap(map[string]any{
		"minimum": min,
		"maximum": max,
	})
	if s.Minimum == nil || s.Maximum == nil {
		t.Fatalf("expected min/max pointers to be set")
	}

	v := DefaultValidator{}
	if err := v.Validate(map[string]any{"i": float32(2)}, &JSONSchema{Type: "object", Properties: map[string]any{"i": map[string]any{"type": "integer"}}}); err != nil {
		t.Fatalf("expected float32 whole number to be integer, got %v", err)
	}
	if err := v.Validate(map[string]any{"i": float32(2.5)}, &JSONSchema{Type: "object", Properties: map[string]any{"i": map[string]any{"type": "integer"}}}); err == nil {
		t.Fatalf("expected float32 non-integer to fail")
	}
}
