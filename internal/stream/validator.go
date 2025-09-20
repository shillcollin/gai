package stream

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

//go:embed schema/gai.events.v1.json
var eventsSchema []byte

var _ = eventsSchema

// ValidateRaw performs structural validation for normalized stream events.
func ValidateRaw(data []byte) error {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	schema, ok := event["schema"].(string)
	if !ok || schema != "gai.events.v1" {
		return fmt.Errorf("unexpected schema %v", event["schema"])
	}
	if _, ok := event["type"].(string); !ok {
		return errors.New("event missing type")
	}
	if seq, ok := event["seq"].(float64); !ok || seq < 0 {
		return fmt.Errorf("invalid seq %v", event["seq"])
	}
	if ts, ok := event["ts"].(string); ok {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			return fmt.Errorf("invalid timestamp: %w", err)
		}
	}
	return nil
}

// Validator accumulates monotonic sequence validation.
type Validator struct {
	lastSeq    int
	seenFinish bool
}

// Validate validates the event.
func (v *Validator) Validate(data []byte) error {
	if err := ValidateRaw(data); err != nil {
		return err
	}
	var event struct {
		Type string `json:"type"`
		Seq  int    `json:"seq"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	if event.Seq <= v.lastSeq {
		return fmt.Errorf("non-increasing seq: %d -> %d", v.lastSeq, event.Seq)
	}
	v.lastSeq = event.Seq

	switch event.Type {
	case "finish", "error":
		if v.seenFinish {
			return errors.New("multiple terminal events detected")
		}
		v.seenFinish = true
	}
	return nil
}

// EnsureFinished verifies a terminal event was received.
func (v *Validator) EnsureFinished() error {
	if !v.seenFinish {
		return errors.New("stream missing terminal event")
	}
	return nil
}
