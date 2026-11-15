package store

import "encoding/json"

// ToJSON marshals v for storage in a TEXT column; nil/empty becomes "null"/"[]".
func ToJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// FromJSON unmarshals a TEXT column into v; empty strings are treated as null.
func FromJSON(s string, v any) {
	if s == "" {
		s = "null"
	}
	_ = json.Unmarshal([]byte(s), v)
}
