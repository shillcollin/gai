package core

import (
	"context"
	"testing"
)

type fakeProvider struct {
	object []byte
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

func (f fakeProvider) StreamObject(context.Context, Request) (*ObjectStreamRaw, error) {
	return nil, nil
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
