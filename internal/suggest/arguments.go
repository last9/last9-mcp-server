package suggest

import (
	"encoding/json"
	"fmt"
)

func PrepareArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil
	}
	changed := false
	for _, pair := range [][2]string{{"name", "query"}, {"entity_type", "type"}} {
		alias, key := pair[0], pair[1]
		value, ok := fields[alias]
		if !ok {
			continue
		}
		if current, exists := fields[key]; exists {
			var a, b *string
			if json.Unmarshal(current, &a) != nil || json.Unmarshal(value, &b) != nil || a == nil || b == nil || *a != *b {
				return nil, fmt.Errorf("conflicting arguments %q and %q: supply one string value", alias, key)
			}
		}
		fields[key] = value
		delete(fields, alias)
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(fields)
}
