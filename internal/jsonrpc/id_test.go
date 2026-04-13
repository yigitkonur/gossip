package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestNormalizeID(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"int", `42`, "42", true},
		{"negative-int", `-5`, "-5", true},
		{"float", `42.0`, "42", true},
		{"non-integer-float", `42.5`, "", false},
		{"string", `"abc"`, "abc", true},
		{"empty-string", `""`, "", true},
		{"null", `null`, "", false},
		{"object", `{"x":1}`, "", false},
		{"empty", ``, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := NormalizeID(json.RawMessage(c.raw))
			if got != c.want || ok != c.ok {
				t.Errorf("NormalizeID(%q) = (%q, %v), want (%q, %v)", c.raw, got, ok, c.want, c.ok)
			}
		})
	}
}
