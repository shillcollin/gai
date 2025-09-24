package obs

import (
	"context"
	"sync"
	"testing"
)

func resetForTest() {
	manager = nil
	managerOnce = sync.Once{}
}

func TestInitWithoutBraintrustOrExporter(t *testing.T) {
	resetForTest()
	shutdown, err := Init(context.Background(), Options{Exporter: ExporterNone})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	resetForTest()
}

func TestInitWithBraintrustDisabledByDefault(t *testing.T) {
	resetForTest()
	opts := DefaultOptions()
	opts.Exporter = ExporterNone
	opts.Braintrust.Enabled = false
	shutdown, err := Init(context.Background(), opts)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	resetForTest()
}
