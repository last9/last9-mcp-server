package traces

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func transformStage(as interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "transform",
		"transforms": []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"$split": []interface{}{"SpanName", "/", 1}},
				"as":       as,
			},
		},
	}
}

func transformAs(stage map[string]interface{}) interface{} {
	return stage["transforms"].([]interface{})[0].(map[string]interface{})["as"]
}

func TestSanitizeTraceJSONQuery_TransformAsStringBecomesArray(t *testing.T) {
	stage := transformStage("endpoint")
	if err := SanitizeTraceJSONQuery([]map[string]interface{}{stage}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := transformAs(stage), []interface{}{"endpoint"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("as = %#v, want %#v", got, want)
	}
}

func TestSanitizeTraceJSONQuery_TransformAsArrayUnchanged(t *testing.T) {
	stage := transformStage([]interface{}{"service", "route"})
	if err := SanitizeTraceJSONQuery([]map[string]interface{}{stage}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := transformAs(stage), []interface{}{"service", "route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("as = %#v, want %#v", got, want)
	}
}

func TestPipelineSchemaHintShowsTransformShape(t *testing.T) {
	for _, want := range []string{"transform", transformShapeExample, "RE2"} {
		if !strings.Contains(tracePipelineSchemaHint, want) {
			t.Errorf("tracePipelineSchemaHint missing %q", want)
		}
	}
}

func TestSanitizeTraceJSONQuery_TransformAsInvalidShape(t *testing.T) {
	for name, as := range map[string]interface{}{
		"missing":           nil,
		"blank string":      " ",
		"empty array":       []interface{}{},
		"number":            1,
		"non-string member": []interface{}{"ok", 2},
		"blank member":      []interface{}{""},
	} {
		t.Run(name, func(t *testing.T) {
			stage := transformStage(as)
			if as == nil {
				delete(stage["transforms"].([]interface{})[0].(map[string]interface{}), "as")
			}
			err := SanitizeTraceJSONQuery([]map[string]interface{}{stage})
			var pipeErr *tracePipelineError
			if !errors.As(err, &pipeErr) {
				t.Fatalf("want tracePipelineError, got %T %v", err, err)
			}
			if pipeErr.Category() != traceCategoryTransformShape {
				t.Fatalf("category=%s want %s", pipeErr.Category(), traceCategoryTransformShape)
			}
			if pipeErr.Path() != "tracejson_query[0].transforms[0].as" {
				t.Fatalf("path=%q", pipeErr.Path())
			}
			if !strings.Contains(err.Error(), `"as":["endpoint"]`) {
				t.Fatalf("error %q does not show the array shape", err)
			}
		})
	}
}
