package entity

import "testing"

func TestParseStructuredNote(t *testing.T) {
	tests := []struct {
		name, content, format, payload string
		ok                             bool
	}{
		{"json payload", `raci/1:{"a":1}`, "raci/1", `{"a":1}`, true},
		{"splits at the first colon", "fmt:a:b", "fmt", "a:b", true},
		{"payload need not be JSON", "kv:key=value", "kv", "key=value", true},
		{"no colon", "raci/1", "", "", false},
		{"blank format", ":payload", "", "", false},
		{"blank payload", "fmt:", "", "", false},
		{"space-only format", "  :payload", "", "", false},
		{"space-only payload", "fmt:  ", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, payload, err := ParseStructuredNote(tt.content)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseStructuredNote(%q) error = %v, want ok = %v", tt.content, err, tt.ok)
			}
			if tt.ok && (format != tt.format || payload != tt.payload) {
				t.Errorf("ParseStructuredNote(%q) = %q, %q; want %q, %q", tt.content, format, payload, tt.format, tt.payload)
			}
		})
	}
}
