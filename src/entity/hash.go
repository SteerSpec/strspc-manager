package entity

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	h := blake3.Sum256(canonical)
	return "blake3:" + hex.EncodeToString(h[:]), nil
}

// canonicalJSON returns the canonical form of an entity file that ComputeHash
// hashes, with every rule_set.hash nulled.
//
// Numbers are read as written, not as float64, so that an integer keeps all its
// digits as it does in Python's json.loads: one past 2^53, or from 1e16, would
// otherwise be rounded or written with an exponent.
func canonicalJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("unmarshaling for hash: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unmarshaling for hash: unexpected data after the top-level value")
	}
	if err := normalizeNumbers(obj); err != nil {
		return nil, fmt.Errorf("unmarshaling for hash: %w", err)
	}

	nullHashes(obj)

	canonical, err := marshalCanonical(obj)
	if err != nil {
		return nil, fmt.Errorf("canonical marshal: %w", err)
	}
	return canonical, nil
}

// normalizeNumbers rewrites, in place, every json.Number below v as Python's
// json.dumps writes the value json.loads reads from it: an integer literal is
// kept as it is, "-0" aside, and any other number becomes the repr of a float.
func normalizeNumbers(v any) error {
	switch val := v.(type) {
	case map[string]any:
		for k, item := range val {
			if n, ok := item.(json.Number); ok {
				norm, err := normalizeNumber(n)
				if err != nil {
					return err
				}
				val[k] = norm
			} else if err := normalizeNumbers(item); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range val {
			if n, ok := item.(json.Number); ok {
				norm, err := normalizeNumber(n)
				if err != nil {
					return err
				}
				val[i] = norm
			} else if err := normalizeNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeNumber(n json.Number) (json.Number, error) {
	s := n.String()
	if !strings.ContainsAny(s, ".eE") {
		if s == "-0" {
			return "0", nil
		}
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("number %s: %w", s, err)
	}
	return json.Number(floatRepr(f)), nil
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
//
// One difference remains: json.Unmarshal turns a lone surrogate escape such as
// "\ud800" into U+FFFD, which Python's json.dumps would keep, so a file that
// holds one hashes differently in the two. Entity files are valid UTF-8 text,
// so none of the org's does; matching it would take a decoder of our own.
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

// marshalFloat formats a float64 matching Python's json.dumps output, except
// that a whole number below 1e16 is written without a decimal point, as Python
// writes an int. canonicalJSON does not come here: it keeps the literal of an
// integer and gives a float the repr of floatRepr.
func marshalFloat(f float64) []byte {
	if f == 0 {
		return []byte("0") // also -0
	}
	r := floatRepr(f)
	return []byte(strings.TrimSuffix(r, ".0"))
}

// floatRepr formats f as Python's repr(float) does, which json.dumps writes:
// the shortest digits that round-trip, a plain decimal from 1e-4 up to 1e16 and
// an exponent outside that range, and ".0" after a whole number in plain form.
func floatRepr(f float64) string {
	exp := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±XX
	e, err := strconv.Atoi(exp[strings.IndexByte(exp, 'e')+1:])
	if err != nil || e < -4 || e >= 16 {
		return exp
	}
	plain := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(plain, ".") {
		plain += ".0"
	}
	return plain
}
