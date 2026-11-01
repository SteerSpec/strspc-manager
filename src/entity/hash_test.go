package entity

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lukechampine.com/blake3"
)

func TestComputeHash(t *testing.T) {
	minimal := []byte(`{
		"entity": {"id": "TST", "title": "Test"},
		"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
		"rules": [], "notes": []
	}`)

	t.Run("valid format", func(t *testing.T) {
		h, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(h, "blake3:") {
			t.Fatalf("expected blake3: prefix, got %q", h)
		}
		if len(h) != len("blake3:")+64 {
			t.Fatalf("expected 64 hex chars after prefix, got %d", len(h)-len("blake3:"))
		}
	})

	t.Run("stable output", func(t *testing.T) {
		h1, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("unstable hash: %q != %q", h1, h2)
		}
	})

	t.Run("hash field excluded", func(t *testing.T) {
		withNull := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": []
		}`)
		withHash := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": "blake3:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"rules": [], "notes": []
		}`)
		h1, err := ComputeHash(withNull)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(withHash)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("hash should be excluded: %q != %q", h1, h2)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ComputeHash([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("sub-entity hash nulled", func(t *testing.T) {
		withSub := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": [],
			"sub_entities": [{
				"entity": {"id": "SUB", "title": "Sub"},
				"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
				"rules": [], "notes": []
			}]
		}`)
		withSubHash := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": [],
			"sub_entities": [{
				"entity": {"id": "SUB", "title": "Sub"},
				"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": "blake3:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
				"rules": [], "notes": []
			}]
		}`)
		h1, err := ComputeHash(withSub)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(withSubHash)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("sub-entity hash should be nulled: %q != %q", h1, h2)
		}
	})
}

func TestComputeHash_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"malformed", `not json`},
		{"empty", ``},
		{"top-level array", `[]`},
		{"top-level string", `"entity"`},
		{"top-level number", `1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := ComputeHash([]byte(tt.input))
			if err == nil || !strings.HasPrefix(err.Error(), "unmarshaling for hash: ") {
				t.Fatalf("ComputeHash(%q) error = %v, want an unmarshaling for hash error", tt.input, err)
			}
			if h != "" {
				t.Errorf("ComputeHash(%q) = %q, want no hash on error", tt.input, h)
			}
		})
	}
}

// TestComputeHash_HashesCanonicalBytes pins the exact bytes that get hashed:
// keys sorted at every level, separators minified, non-ASCII escaped, and
// rule_set.hash nulled in the file and in every sub-entity.
func TestComputeHash_HashesCanonicalBytes(t *testing.T) {
	input := []byte(`{
		"z": "é\n",
		"rule_set": {"version": "1.0.0", "hash": "blake3:stale"},
		"revision": 2,
		"sub_entities": [{"rule_set": {"hash": "blake3:old"}}, "not an object"],
		"note": "<&>"
	}`)
	canonical := `{"note":"<&>","revision":2,"rule_set":{"hash":null,"version":"1.0.0"},` +
		`"sub_entities":[{"rule_set":{"hash":null}},"not an object"],"z":"\u00e9\n"}`

	sum := blake3.Sum256([]byte(canonical))
	want := "blake3:" + hex.EncodeToString(sum[:])

	got, err := ComputeHash(input)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if got != want {
		t.Errorf("ComputeHash = %q, want the hash of %s = %q", got, canonical, want)
	}
}

// The want columns of the tests below are what Python's
// json.dumps(value, sort_keys=True, separators=(",", ":")) prints, the output
// marshalCanonical must reproduce for hashes to agree across implementations.
// ComputeHash decodes every JSON number to float64, so a whole-number float
// up to 1e15 stands for the JSON integer literal; the larger ones stand for
// exponent-form literals such as 1e16, which Python reads as floats.

func TestMarshalString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `""`},
		{"plain ascii", "plain", `"plain"`},
		{"double quote", `a"b`, `"a\"b"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"named control escapes", "\b\f\n\r\t", `"\b\f\n\r\t"`},
		{"NUL", "\x00", `"\u0000"`},
		{"control without a short escape", "\x01", `"\u0001"`},
		{"last control character", "\x1f", `"\u001f"`},
		{"space is the first literal", " ", `" "`},
		{"tilde is the last literal", "~", `"~"`},
		{"DEL is escaped", "\x7f", `"\u007f"`},
		{"first non-ASCII", "\u0080", `"\u0080"`},
		{"two-byte rune", "é", `"\u00e9"`},
		{"three-byte rune", "€", `"\u20ac"`},
		{"replacement character", "\ufffd", `"\ufffd"`},
		{"last BMP code point", "\uffff", `"\uffff"`},
		{"first surrogate pair", "\U00010000", `"\ud800\udc00"`},
		{"emoji surrogate pair", "😀", `"\ud83d\ude00"`},
		{"last code point", "\U0010FFFF", `"\udbff\udfff"`},
		{"HTML characters stay literal", "<>&", `"<>&"`},
		{"slash and apostrophe stay literal", `/'`, `"/'"`},
		{"mixed widths", "aé\"\n😀z", `"a\u00e9\"\n\ud83d\ude00z"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(marshalString(tt.in)); got != tt.want {
				t.Errorf("marshalString(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarshalFloat(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "0"},
		{"integer", 1, "1"},
		{"negative integer", -7, "-7"},
		{"largest integer form", 1e15, "1000000000000000"},
		{"smallest integer form", -1e15, "-1000000000000000"},
		{"fraction", 1.5, "1.5"},
		{"negative fraction", -0.25, "-0.25"},
		{"inexact fraction", 0.1, "0.1"},
		{"small fraction", 0.0001, "0.0001"},
		{"fraction below the exponent threshold", 123456.5, "123456.5"},
		{"small exponent", 1e-5, "1e-05"},
		{"small mantissa and exponent", 1.5e-7, "1.5e-07"},
		{"first value past the integer form", 1e16, "1e+16"},
		{"large exponent", 1e20, "1e+20"},
		{"largest float", 1.7976931348623157e308, "1.7976931348623157e+308"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(marshalFloat(tt.in)); got != tt.want {
				t.Errorf("marshalFloat(%v) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarshalCanonical(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, `null`},
		{"true", true, `true`},
		{"false", false, `false`},
		{"string", "é", `"\u00e9"`},
		{"float", 1.5, `1.5`},
		{"json.Number keeps its literal", json.Number("1.50"), `1.50`},
		{"other types fall back to encoding/json", 42, `42`},
		{"empty object", map[string]any{}, `{}`},
		{"empty array", []any{}, `[]`},
		{"array keeps its order", []any{"b", 1.0, nil, false, "a"}, `["b",1,null,false,"a"]`},
		{
			"keys sorted at every level, by code point",
			map[string]any{
				"z":      1.0,
				"é":      2.0,
				"a":      map[string]any{"b": []any{true, false, nil}, "a": "x"},
				"😀":      3.0,
				"\uffff": 4.0,
			},
			`{"a":{"a":"x","b":[true,false,null]},"z":1,"\u00e9":2,"\uffff":4,"\ud83d\ude00":3}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalCanonical(tt.in)
			if err != nil {
				t.Fatalf("marshalCanonical(%v): %v", tt.in, err)
			}
			if string(got) != tt.want {
				t.Errorf("marshalCanonical(%v) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestMarshalCanonical_UnsupportedValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{"top level", make(chan int)},
		{"object member", map[string]any{"k": make(chan int)}},
		{"array item", []any{"ok", make(chan int)}},
		{"nested", map[string]any{"k": []any{map[string]any{"k": make(chan int)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalCanonical(tt.in)
			var unsupported *json.UnsupportedTypeError
			if !errors.As(err, &unsupported) {
				t.Fatalf("marshalCanonical error = %v, want *json.UnsupportedTypeError", err)
			}
			if got != nil {
				t.Errorf("marshalCanonical = %s, want no output on error", got)
			}
		})
	}
}

// TestComputeHash_TestdataFiles verifies that every testdata/valid/*.json file
// with a non-null hash field has a hash that matches ComputeHash output.
// This prevents stale hashes from going undetected.
func TestComputeHash_TestdataFiles(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	validDir := filepath.Join(filepath.Dir(file), "testdata", "valid")

	entries, err := os.ReadDir(validDir)
	if err != nil {
		t.Fatalf("reading testdata/valid: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(validDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}

		var probe struct {
			RuleSet struct {
				Hash *string `json:"hash"`
			} `json:"rule_set"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		if probe.RuleSet.Hash == nil || *probe.RuleSet.Hash == "" {
			continue
		}

		computed, err := ComputeHash(data)
		if err != nil {
			t.Errorf("%s: ComputeHash error: %v", entry.Name(), err)
			continue
		}
		if computed != *probe.RuleSet.Hash {
			t.Errorf("%s: hash mismatch\n  stored:   %s\n  computed: %s", entry.Name(), *probe.RuleSet.Hash, computed)
		}
		checked++
	}

	if checked == 0 {
		t.Fatal("no testdata files with non-null hashes found — test is not exercising anything")
	}
}
