package stream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedSchemaMatchesDocs(t *testing.T) {
	docPath := filepath.Join("..", "..", "docs", "schema", "gai.events.v1.json")
	docBytes, err := os.ReadFile(docPath)
	if err != nil {
		t.Skipf("unable to read docs schema: %v", err)
	}
	if string(docBytes) != string(eventsSchema) {
		t.Fatalf("embedded schema mismatch with docs/schema/gai.events.v1.json")
	}
}
