package json

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	input := `{"name": "n2n", "version": 3.0, "features": ["JSON", "Regex"]}`
	obj, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check that the parsed object contains expected fields.
	if obj["name"] != "n2n" {
		t.Errorf("Expected name 'n2n', got %v", obj["name"])
	}
	if ver, ok := obj["version"].(float64); !ok || ver != 3.0 {
		t.Errorf("Expected version 3.0, got %v", obj["version"])
	}
	expectedFeatures := []interface{}{"JSON", "Regex"}
	if !reflect.DeepEqual(obj["features"], expectedFeatures) {
		t.Errorf("Expected features %v, got %v", expectedFeatures, obj["features"])
	}
}
