package types

import (
	"encoding/json"
	"testing"
)

func TestNestedMetadata(t *testing.T) {
	metadata := Metadata{
		Meta: GenericMetadata{
			"submeta": GenericMetadata{
				"nested": GenericMetadata{
					"key": "value",
				},
			},
		},
	}
	marshalled, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	expected := `{"title":{},"description":{},"media":{"header":"","logo":""},"questions":null,"type":{"name":"","properties":{}},"version":"","meta":{"submeta":{"nested":{"key":"value"}}}}`
	if string(marshalled) != expected {
		t.Errorf("expected %s, got %s", expected, marshalled)
	}
	var unmarshalled Metadata
	if err := json.Unmarshal(marshalled, &unmarshalled); err != nil {
		t.Fatalf("failed to unmarshal metadata: %v", err)
	}
	submeta, ok := unmarshalled.Meta["submeta"].(GenericMetadata)
	if !ok {
		t.Fatalf("expected submeta to be GenericMetadata, got %T", unmarshalled.Meta["submeta"])
	}
	nested, ok := submeta["nested"].(GenericMetadata)
	if !ok {
		t.Fatalf("expected nested to be GenericMetadata, got %T", submeta["nested"])
	}
	keyValue, ok := nested["key"].(string)
	if !ok {
		t.Fatalf("expected key to be string, got %T", nested["key"])
	}
	if keyValue != "value" {
		t.Errorf("expected key to be 'value', got %s", keyValue)
	}
}
