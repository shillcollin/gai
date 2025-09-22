package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shillcollin/gai/core"
)

func TestSSEWithPolicyFilters(t *testing.T) {
	stream := core.NewStream(context.Background(), 4)
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)

	go func() {
		done <- SSEWithPolicy(recorder, stream, Policy{SendStart: true, SendFinish: true})
	}()

	stream.Push(core.StreamEvent{Type: core.EventStart, Schema: "gai.events.v1", Seq: 1})
	stream.Push(core.StreamEvent{Type: core.EventReasoningDelta, Schema: "gai.events.v1", Seq: 2})
	stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", Seq: 3})
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("sse error: %v", err)
	}

	body := recorder.Body.String()
	if strings.Count(body, "reasoning.delta") != 0 {
		t.Fatalf("expected reasoning events filtered, got %s", body)
	}
	if strings.Count(body, "\n\n") != 2 {
		t.Fatalf("expected two events, got %q", body)
	}
}

func TestNDJSONWriter(t *testing.T) {
	stream := core.NewStream(context.Background(), 4)
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { done <- NDJSON(recorder, stream) }()
	stream.Push(core.StreamEvent{Type: core.EventStart, Schema: "gai.events.v1", Seq: 1})
	stream.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", Seq: 2})
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("ndjson error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	var event core.StreamEvent
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode line: %v", err)
	}
	if event.Type != core.EventStart {
		t.Fatalf("unexpected first event %v", event.Type)
	}
}

func TestReaderNDJSON(t *testing.T) {
	payload := strings.Join([]string{
		`{"type":"start","schema":"gai.events.v1","seq":1}`,
		`{"type":"finish","schema":"gai.events.v1","seq":2}`,
	}, "\n")
	r := NewReader(io.NopCloser(strings.NewReader(payload+"\n")), FormatNDJSON)

	event, err := r.Read()
	if err != nil {
		t.Fatalf("read first event: %v", err)
	}
	if event.Type != core.EventStart {
		t.Fatalf("expected start, got %s", event.Type)
	}
	if _, err = r.Read(); err != nil {
		t.Fatalf("read finish: %v", err)
	}
	if _, err = r.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReaderSSE(t *testing.T) {
	data := "data: {\"type\":\"start\",\"schema\":\"gai.events.v1\",\"seq\":1}\n\n" +
		"data: {\"type\":\"finish\",\"schema\":\"gai.events.v1\",\"seq\":2}\n\n"
	r := NewReader(io.NopCloser(strings.NewReader(data)), FormatSSE)
	if _, err := r.Read(); err != nil {
		t.Fatalf("read start: %v", err)
	}
	if _, err := r.Read(); err != nil {
		t.Fatalf("read finish: %v", err)
	}
	if _, err := r.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReaderMissingFinish(t *testing.T) {
	payload := `{"type":"start","schema":"gai.events.v1","seq":1}`
	r := NewReader(io.NopCloser(strings.NewReader(payload+"\n")), FormatNDJSON)
	if _, err := r.Read(); err != nil {
		t.Fatalf("read start: %v", err)
	}
	if _, err := r.Read(); err == nil {
		t.Fatalf("expected error for missing finish")
	}
}

func TestMaskError(t *testing.T) {
	s := core.NewStream(context.Background(), 1)
	recorder := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { done <- SSEWithPolicy(recorder, s, Policy{MaskErrors: true}) }()
	s.Push(core.StreamEvent{Type: core.EventError, Schema: "gai.events.v1", Seq: 1, Error: io.EOF})
	s.Push(core.StreamEvent{Type: core.EventFinish, Schema: "gai.events.v1", Seq: 2})
	_ = s.Close()
	if err := <-done; err != nil {
		t.Fatalf("sse error: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "error_masked") {
		t.Fatalf("expected masked error marker, got %q", recorder.Body.String())
	}
}
