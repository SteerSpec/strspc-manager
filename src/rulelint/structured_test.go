package rulelint

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// structuredFile is an entity with one rule and a `structured` note per content.
func structuredFile(contents ...string) *entity.File {
	f := mappedFile("STRUCT", nil, contents...)
	for i := range f.Notes {
		f.Notes[i].Type = entity.NoteTypeStructured
	}
	return f
}

func TestStructuredNotes(t *testing.T) {
	tests := []struct {
		name     string
		contents []string
		want     map[string]int
	}{
		{"one well formed note", []string{`raci/1:{"a":1}`}, nil},
		{"payload need not be JSON", []string{"kv:key=value"}, nil},
		{"no notes", nil, nil},
		{"malformed content", []string{"no format"}, map[string]int{"RL042": 1}},
		{"blank payload", []string{"fmt: "}, map[string]int{"RL042": 1}},
		{"two notes on a rule", []string{"a:1", "a:2"}, map[string]int{"RL043": 1}},
		{"three notes on a rule", []string{"a:1", "a:2", "a:3"}, map[string]int{"RL043": 2}},
		{"two malformed notes", []string{"x", "y"}, map[string]int{"RL042": 2, "RL043": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codes(New().LintFile(structuredFile(tt.contents...)))
			for _, code := range []string{"RL042", "RL043"} {
				if got[code] != tt.want[code] {
					t.Errorf("%s fired %d times, want %d", code, got[code], tt.want[code])
				}
			}
		})
	}
}
