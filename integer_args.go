package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Models sometimes quote integer ids ({"channel_id": "42"}) and the SDK validates
// arguments strictly before a handler runs, so such strings become numbers first.
var integerString = regexp.MustCompile(`^-?\d+$`)

// inputSchemaFor returns the schema the SDK validates a tool's arguments against: the
// tool's own, else the one it infers from In.
func inputSchemaFor[In any](tool *mcp.Tool) (*jsonschema.Schema, error) {
	switch s := tool.InputSchema.(type) {
	case *jsonschema.Schema:
		return s, nil
	case nil:
		rt := reflect.TypeFor[In]()
		if rt == reflect.TypeFor[any]() {
			return &jsonschema.Schema{Type: "object"}, nil
		}
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		return jsonschema.ForType(rt, &jsonschema.ForOptions{})
	default:
		data, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, err
		}
		return &schema, nil
	}
}

// integerArgsMiddleware coerces number-like strings in tool's integer arguments.
func integerArgsMiddleware(tool string, schema *jsonschema.Schema) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && call.Params.Name == tool {
				call.Params.Arguments = coerceIntegerArguments(schema, call.Params.Arguments)
			}
			return next(ctx, method, req)
		}
	}
}

// coerceIntegerArguments returns args with every number-like string under an integer
// schema replaced by its number. Anything it cannot read passes through unchanged for
// the SDK to reject.
func coerceIntegerArguments(schema *jsonschema.Schema, args json.RawMessage) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return args
	}
	coerced, changed := coerceIntegerStrings(schema, value)
	if !changed {
		return args
	}
	out, err := json.Marshal(coerced)
	if err != nil {
		return args
	}
	return out
}

func coerceIntegerStrings(schema *jsonschema.Schema, value any) (any, bool) {
	if schema == nil {
		return value, false
	}
	switch v := value.(type) {
	case string:
		if !acceptsOnlyInteger(schema) || !integerString.MatchString(v) {
			return value, false
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return value, false
		}
		return n, true
	case map[string]any:
		changed := false
		for key, item := range v {
			if coerced, ok := coerceIntegerStrings(schema.Properties[key], item); ok {
				v[key], changed = coerced, true
			}
		}
		return v, changed
	case []any:
		changed := false
		for i, item := range v {
			if coerced, ok := coerceIntegerStrings(schema.Items, item); ok {
				v[i], changed = coerced, true
			}
		}
		return v, changed
	}
	return value, false
}

// acceptsOnlyInteger reports whether schema admits integers but not strings, so a
// string value is certain to fail validation and safe to coerce.
func acceptsOnlyInteger(schema *jsonschema.Schema) bool {
	types := schema.Types
	if schema.Type != "" {
		types = append([]string{schema.Type}, types...)
	}
	return slices.Contains(types, "integer") && !slices.Contains(types, "string")
}

func hasIntegerProperty(schema *jsonschema.Schema) bool {
	if schema == nil {
		return false
	}
	if acceptsOnlyInteger(schema) || hasIntegerProperty(schema.Items) {
		return true
	}
	for _, prop := range schema.Properties {
		if hasIntegerProperty(prop) {
			return true
		}
	}
	return false
}

// addIntegerArgsCoercion installs integerArgsMiddleware for tool when its input schema
// has an integer property anywhere.
func addIntegerArgsCoercion[In any](server *mcp.Server, tool *mcp.Tool) error {
	schema, err := inputSchemaFor[In](tool)
	if err != nil {
		return fmt.Errorf("input schema: %w", err)
	}
	if hasIntegerProperty(schema) {
		server.AddReceivingMiddleware(integerArgsMiddleware(tool.Name, schema))
	}
	return nil
}
