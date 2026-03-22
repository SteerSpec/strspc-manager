package rulelint

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"lukechampine.com/blake3"
)

// computeHash computes the Blake3 hash of an entity file's canonical JSON.
// It nulls out rule_set.hash recursively, serializes to sorted-key minified
// JSON, and returns "blake3:<64 hex chars>".
func computeHash(data []byte) (string, error) {
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
			keyJSON, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, keyJSON...)
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

	default:
		return json.Marshal(v)
	}
}
