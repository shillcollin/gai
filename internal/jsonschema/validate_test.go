package jsonschema

import (
	"encoding/json"
	"testing"
)

type validateSample struct {
	Title string  `json:"title"`
	Score float64 `json:"score" minimum:"0" maximum:"1"`
}

func TestValidator(t *testing.T) {
	sch, err := Derive[validateSample]()
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	validator, err := Compile(sch)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	payload := validateSample{Title: "ok", Score: 0.5}
	data, _ := json.Marshal(payload)
	if err := validator.Validate(data); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	payload.Score = 2
	data, _ = json.Marshal(payload)
	if err := validator.Validate(data); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRepairJSON(t *testing.T) {
	raw := []byte("{\"title\":\"ok\",}\n")
	fixed, err := RepairJSON(raw)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if string(fixed) != "{\"title\":\"ok\"}" {
		t.Fatalf("unexpected output: %s", fixed)
	}
}
