package core

import (
	"context"
	"testing"
)

type fakeProvider struct {
	object []byte
	chunks []string
}

func (f fakeProvider) GenerateText(context.Context, Request) (*TextResult, error) {
	return nil, nil
}

func (f fakeProvider) StreamText(context.Context, Request) (*Stream, error) {
	return nil, nil
}

func (f fakeProvider) GenerateObject(context.Context, Request) (*ObjectResultRaw, error) {
	return &ObjectResultRaw{JSON: f.object, Model: "fake", Provider: "fake"}, nil
}

func (f fakeProvider) StreamObject(ctx context.Context, _ Request) (*ObjectStreamRaw, error) {
	if len(f.chunks) == 0 {
		return nil, nil
	}
	stream := NewStream(ctx, len(f.chunks)+2)
	go func() {
		defer stream.Close()
		for _, chunk := range f.chunks {
			stream.Push(StreamEvent{Type: EventTextDelta, TextDelta: chunk, Schema: "gai.events.v1"})
		}
		stream.Push(StreamEvent{Type: EventFinish, Schema: "gai.events.v1", Model: "fake-stream", Provider: "fake", Usage: Usage{}})
	}()
	return NewObjectStreamRaw(stream), nil
}

func (f fakeProvider) Capabilities() Capabilities { return Capabilities{} }

type sampleStruct struct {
	Title string  `json:"title"`
	Score float64 `json:"score" minimum:"0" maximum:"1"`
}

func TestGenerateObjectTyped(t *testing.T) {
	provider := fakeProvider{object: []byte(`{"title":"example","score":0.4}`)}
	res, err := GenerateObjectTyped[sampleStruct](context.Background(), provider, Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Value.Score != 0.4 {
		t.Fatalf("unexpected score: %v", res.Value.Score)
	}
}

func TestGenerateObjectTypedWithRepair(t *testing.T) {
	provider := fakeProvider{object: []byte(`{"title":"example","score":0.5,}`)}
	_, err := GenerateObjectTyped[sampleStruct](context.Background(), provider, Request{}, StructuredOptions{Mode: StrictRepair})
	if err != nil {
		t.Fatalf("expected repair to succeed: %v", err)
	}
}

func TestStreamObjectTyped(t *testing.T) {
	provider := fakeProvider{chunks: []string{"{\"title\":\"example\",", "\"score\":0.4}"}}
	stream, err := StreamObjectTyped[sampleStruct](context.Background(), provider, Request{})
	if err != nil {
		t.Fatalf("StreamObjectTyped error: %v", err)
	}
	res, err := stream.Final()
	if err != nil {
		t.Fatalf("stream final error: %v", err)
	}
	if res.Value.Score != 0.4 {
		t.Fatalf("unexpected score: %v", res.Value.Score)
	}
	if len(res.RawJSON) == 0 {
		t.Fatalf("expected raw json to be populated")
	}
}
