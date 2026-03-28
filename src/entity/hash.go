package entity

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"lukechampine.com/blake3"
)

// ComputeHash computes the Blake3 hash of an entity file's canonical JSON.
// It nulls out rule_set.hash recursively, serializes to sorted-key minified
// JSON, and returns "blake3:<64 hex chars>".
func ComputeHash(data []byte) (string, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", fmt.Errorf("unmarshaling for hash: %w", err)
	}

	nullHashes(obj)

	canonical, err := marshalCanonical(obj)
	if err != nil {
		return "", fmt.Errorf("canonical marshal: %w", err)
	}

	h := blake3.Sum256(canonical)
	return "blake3:" + hex.EncodeToString(h[:]), nil
}

// nullHashes recursively sets rule_set.hash to null in the object and all
// sub_entities.
func nullHashes(obj map[string]any) {
	if rs, ok := obj["rule_set"].(map[string]any); ok {
		rs["hash"] = nil
	}
	if subs, ok := obj["sub_entities"].([]any); ok {
		for _, sub := range subs {
			if m, ok := sub.(map[string]any); ok {
				nullHashes(m)
			}
		}
	}
}

// marshalCanonical produces minified JSON with sorted keys at every level.
// The output matches Python's json.dumps(sort_keys=True, separators=(",",":"))
// with ensure_ascii=True (the default), so that Blake3 hashes are identical
// across Go and Python implementations.
func marshalCanonical(v any) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, marshalString(k)...)
			buf = append(buf, ':')
			valJSON, err := marshalCanonical(val[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, valJSON...)
		}
		buf = append(buf, '}')
		return buf, nil

	case []any:
		buf := []byte{'['}
		for i, item := range val {
			if i > 0 {
				buf = append(buf, ',')
			}
			itemJSON, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, itemJSON...)
		}
		buf = append(buf, ']')
		return buf, nil

	case string:
		return marshalString(val), nil

	case nil:
		return []byte("null"), nil

	case bool:
		if val {
			return []byte("true"), nil
		}
		return []byte("false"), nil

	case json.Number:
		return []byte(val.String()), nil

	case float64:
		return marshalFloat(val), nil

	default:
		return json.Marshal(v)
	}
}

// marshalString produces a JSON string matching Python's json.dumps with
// ensure_ascii=True: ASCII characters are written literally, non-ASCII
// characters are escaped as \uXXXX. HTML-sensitive characters (<, >, &)
// are NOT escaped (matching Python's default behavior).
func marshalString(s string) []byte {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			// Control characters: \u00XX
			fmt.Fprintf(&b, `\u%04x`, r)
		case r > 0x7e:
			// Non-ASCII: escape as \uXXXX (or surrogate pair for > U+FFFF)
			if r <= 0xFFFF {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				// Surrogate pair for supplementary plane characters
				r -= 0x10000
				hi := 0xD800 + (r>>10)&0x3FF
				lo := 0xDC00 + r&0x3FF
				fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
			}
		default:
			b.WriteRune(r)
		}
		i += size
	}
	b.WriteByte('"')
	return []byte(b.String())
}

// marshalFloat formats a float64 matching Python's json.dumps output.
// Integers are written without decimal point, others use compact representation.
func marshalFloat(f float64) []byte {
	if f == float64(int64(f)) && f >= -1e15 && f <= 1e15 {
		return []byte(strconv.FormatInt(int64(f), 10))
	}
	return []byte(strconv.FormatFloat(f, 'g', -1, 64))
}
