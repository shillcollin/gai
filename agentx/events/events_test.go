package events

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileEmitterEmit(t *testing.T) {
	dir := t.TempDir()
	emitter, err := NewFileEmitter(dir)
	if err != nil {
		t.Fatalf("new emitter: %v", err)
	}

	evt := AgentEventV1{ID: "ev1", Type: TypePhaseStart, Ts: 1234}
	if err := emitter.Emit(evt); err != nil {
		t.Fatalf("emit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "events.ndjson"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected data written")
	}
}

func TestValidateSuccess(t *testing.T) {
	evt := AgentEventV1{
		Version: SchemaVersionV1,
		ID:      "ev_123",
		Type:    TypePhaseStart,
		Ts:      1700000000000,
	}
	if err := evt.Validate(); err != nil {
		t.Fatalf("expected valid event, got %v", err)
	}
}

func TestValidateMissingFields(t *testing.T) {
	tests := []struct {
		name string
		evt  AgentEventV1
	}{
		{"missing_version", AgentEventV1{ID: "id", Type: TypePhaseStart, Ts: 1}},
		{"wrong_version", AgentEventV1{Version: "other", ID: "id", Type: TypePhaseStart, Ts: 1}},
		{"missing_type", AgentEventV1{Version: SchemaVersionV1, ID: "id", Ts: 1}},
		{"missing_id", AgentEventV1{Version: SchemaVersionV1, Type: TypePhaseStart, Ts: 1}},
		{"missing_ts", AgentEventV1{Version: SchemaVersionV1, ID: "id", Type: TypePhaseStart}},
		{"negative_ts", AgentEventV1{Version: SchemaVersionV1, ID: "id", Type: TypePhaseStart, Ts: -1}},
	}

	for _, tc := range tests {
		if err := tc.evt.Validate(); err == nil {
			t.Fatalf("%s: expected validation error", tc.name)
		}
	}
}
