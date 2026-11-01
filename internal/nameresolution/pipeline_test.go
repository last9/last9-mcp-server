package nameresolution

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestServicePipeline(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		want      bool
	}{
		{"body alternatives", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$or":[{"$eq":["Body","timeout"]},{"$eq":["Body","failure"]}]}]}}]`, true},
		{"later environment", `[{"type":"filter","query":{"$eq":["ServiceName","checkuot"]}},{"type":"parse","parser":"json","field":"Body","labels":{"env":"env"}},{"type":"filter","query":{"$eq":["resources['deployment.environment']","prod"]}}]`, false},
		{"direct", `[{"type":"filter","query":{"$eq":["ServiceName","checkuot"]}}]`, true},
		{"scoped conjunction", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$ieq":["resources['deployment.environment']","prod"]},{"$eq":["Body","checkuot"]}]}}]`, true},
		{"where alias", `[{"type":"where","query":{"$eq":["ServiceName","checkuot"]}}]`, true},
		{"regex", `[{"type":"filter","query":{"$regex":["ServiceName","checkuot"]}}]`, false},
		{"negative", `[{"type":"filter","query":{"$neq":["ServiceName","checkuot"]}}]`, false},
		{"case insensitive", `[{"type":"filter","query":{"$ieq":["ServiceName","checkuot"]}}]`, false},
		{"literal", `[{"type":"filter","query":{"$eq":["Body","checkuot"]}}]`, false},
		{"or", `[{"type":"filter","query":{"$or":[{"$eq":["ServiceName","checkuot"]},{"$eq":["ServiceName","billing"]}]}}]`, false},
		{"conflicting names", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$eq":["ServiceName","checkout"]}]}}]`, false},
		{"derived field", `[{"type":"parse","labels":{"Body":"ServiceName"}},{"type":"filter","query":{"$eq":["ServiceName","checkuot"]}}]`, false},
		{"aggregate field", `[{"type":"aggregate","groupby":{"ServiceName":"ServiceName"}},{"type":"filter","query":{"$eq":["ServiceName","checkuot"]}}]`, false},
		{"service plus exclusion", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$neq":["ServiceName","checkout"]}]}}]`, false},
		{"env disjunction", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","checkuot"]},{"$or":[{"$eq":["resources['deployment.environment']","prod"]},{"$eq":["resources['deployment.environment']","staging"]}]}]}}]`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var pipeline []map[string]any
			if err := json.Unmarshal([]byte(tt.raw), &pipeline); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(pipeline)
			prepared, ok := PrepareServicePipeline(pipeline)
			if ok != tt.want {
				t.Fatalf("eligible=%v; want %v", ok, tt.want)
			}
			if !ok {
				return
			}
			if prepared.Requested != "checkuot" {
				t.Fatalf("requested=%s", prepared.Requested)
			}
			corrected := prepared.WithName("checkout")
			raw, _ := json.Marshal(corrected)
			var want []map[string]any
			json.Unmarshal(before, &want)
			rewriteFirstService(want, "checkout")
			if !reflect.DeepEqual(corrected, want) {
				t.Fatalf("rewritten=%s; want %v", raw, want)
			}
			after, _ := json.Marshal(pipeline)
			if string(before) != string(after) {
				t.Fatal("modified caller pipeline")
			}
			scope, _ := json.Marshal(prepared.Scope)
			expected := `[{"query":{},"type":"filter"}]`
			if tt.name == "scoped conjunction" {
				expected = `[{"query":{"$and":[{"$ieq":["resources['deployment.environment']","prod"]}]},"type":"filter"}]`
			}
			if string(scope) != expected {
				t.Fatalf("scope=%s; want %s", scope, expected)
			}
		})
	}
}

func rewriteFirstService(pipeline []map[string]any, name string) {
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "$eq" {
					if pair, ok := item.([]any); ok && len(pair) == 2 && pair[0] == "ServiceName" {
						pair[1] = name
						return
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	for _, stage := range pipeline {
		walk(stage)
	}
}
