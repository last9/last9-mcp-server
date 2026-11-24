package logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
)

func PrepareGetLogsArguments(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil
	}
	alias, ok := fields["pipeline"]
	if !ok {
		return raw, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(alias), []byte("[")) {
		return nil, errors.New("argument \"pipeline\" must be an array of stages")
	}
	if canonical, exists := fields["logjson_query"]; exists {
		if !equalPipelineInputs(canonical, alias) {
			return nil, errors.New("conflicting arguments \"pipeline\" and \"logjson_query\": supply the same stage array or use only logjson_query")
		}
	} else {
		fields["logjson_query"] = alias
	}
	delete(fields, "pipeline")
	return json.Marshal(fields)
}

func equalPipelineInputs(a, b json.RawMessage) bool {
	decode := func(raw json.RawMessage) any {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var value any
		// RawMessage values came from a successfully decoded arguments object.
		_ = dec.Decode(&value)
		return value
	}
	return reflect.DeepEqual(decode(a), decode(b))
}
