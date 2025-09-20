package jsonschema

import (
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count" minimum:"0"`
}

func TestDerive(t *testing.T) {
	schema, err := Derive[sample]()
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("expected object type")
	}
	if len(schema.Required) != 2 {
		t.Fatalf("expected required fields")
	}
}
