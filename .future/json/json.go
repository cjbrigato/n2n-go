// Package json provides a minimal JSON parser interface similar to the original C implementation.
// It wraps Go's standard encoding/json package for parsing JSON strings.
package json

import "encoding/json"

// JSONObject represents a JSON object as a map from string keys to arbitrary values.
type JSONObject map[string]interface{}

// Parse takes a JSON-formatted string and returns a JSONObject.
// It returns an error if the JSON is invalid.
func Parse(str string) (JSONObject, error) {
	var obj JSONObject
	err := json.Unmarshal([]byte(str), &obj)
	return obj, err
}
