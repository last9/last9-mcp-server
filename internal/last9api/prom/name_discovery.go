package prom

import (
	"encoding/json"
	"fmt"
	"strings"
)

func decodeDiscoveryNames(raw []byte) ([]string, error) {
	var names []*string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("name discovery: invalid listing: %w", err)
	}
	if names == nil || len(names) > 10000 {
		return nil, fmt.Errorf("name discovery: missing or oversized listing")
	}
	out := make([]string, len(names))
	for i, name := range names {
		if name == nil || strings.TrimSpace(*name) == "" || len(*name) > 512 {
			return nil, fmt.Errorf("name discovery: invalid listing entry")
		}
		out[i] = *name
	}
	return out, nil
}

func checkDiscoveryMetadata(fields map[string]json.RawMessage) error {
	if raw, ok := fields["status"]; ok && string(raw) != `"success"` {
		return fmt.Errorf("name discovery: non-success status")
	}
	for _, key := range []string{"warnings", "error", "errors", "partial", "truncated"} {
		if raw, ok := fields[key]; ok {
			value := strings.TrimSpace(string(raw))
			if value != "null" && value != "false" && value != "[]" {
				return fmt.Errorf("name discovery: listing reports %s", key)
			}
		}
	}
	return nil
}
