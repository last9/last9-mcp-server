package suggest

import (
	"encoding/json"
	"errors"
	"fmt"
)

// PrepareArguments renames the name and entity_type aliases to query and type before the
// SDK rejects them as unknown keys. An alias sent beside its canonical key must carry the
// same string, so a call never silently drops one of two different values.
func PrepareArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil
	}
	if _, ok := fields["service_name"]; ok {
		return nil, errors.New(`unexpected additional properties ["service_name"]. To look up a service name, pass it as query with type "service". To find a log attribute within a service, pass the attribute as query with type "log_attribute" and the service name as service.`)
	}
	changed := false
	for _, pair := range [][2]string{{"name", "query"}, {"entity_type", "type"}} {
		alias, key := pair[0], pair[1]
		value, ok := fields[alias]
		if !ok {
			continue
		}
		var a, b *string
		if current, exists := fields[key]; exists {
			if json.Unmarshal(current, &a) != nil || json.Unmarshal(value, &b) != nil || a == nil || b == nil || *a != *b {
				return nil, fmt.Errorf("conflicting arguments %q and %q: supply one string value", alias, key)
			}
		} else if json.Unmarshal(value, &b) != nil || b == nil {
			return nil, fmt.Errorf("argument %q must be a string", alias)
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
