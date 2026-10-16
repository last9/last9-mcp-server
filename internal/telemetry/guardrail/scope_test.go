package guardrail

import (
	"encoding/json"
	"testing"
)

func pipelineFromJSON(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var p []map[string]any
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return p
}

func TestPipelineScoped(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		want     bool
	}{
		{"service eq under and", `[{"type":"filter","query":{"$and":[{"$eq":["ServiceName","api"]},{"$containsWords":["Body","timeout"]}]}}]`, true},
		{"bare service eq", `[{"type":"filter","query":{"$eq":["ServiceName","api"]}}]`, true},
		{"case-insensitive service eq", `[{"type":"filter","query":{"$ieq":["ServiceName","API"]}}]`, true},
		{"resource service name", `[{"type":"filter","query":{"$eq":["resources['service.name']","api"]}}]`, true},
		{"k8s deployment", `[{"type":"filter","query":{"$eq":["resources['k8s.deployment.name']","api"]}}]`, true},
		{"trace id", `[{"type":"filter","query":{"$eq":["TraceId","4bf92f3577b34da6a3ce929d0e0e4736"]}}]`, true},
		{"or of service variants", `[{"type":"filter","query":{"$or":[{"$eq":["ServiceName","api"]},{"$eq":["ServiceName","api-canary"]}]}}]`, true},
		{"nested and inside or", `[{"type":"filter","query":{"$or":[{"$and":[{"$eq":["ServiceName","a"]}]},{"$eq":["ServiceName","b"]}]}}]`, true},
		{"scope in second filter stage", `[{"type":"parse","parser":"json"},{"type":"filter","query":{"$eq":["ServiceName","api"]}}]`, true},

		{"empty pipeline", `[]`, false},
		{"body search only", `[{"type":"filter","query":{"$and":[{"$containsWords":["Body","timeout"]}]}}]`, false},
		{"environment is not a scope", `[{"type":"filter","query":{"$eq":["resources['deployment.environment']","prod"]}}]`, false},
		{"empty service value", `[{"type":"filter","query":{"$eq":["ServiceName",""]}}]`, false},
		{"blank service value", `[{"type":"filter","query":{"$eq":["ServiceName","  "]}}]`, false},
		{"service neq", `[{"type":"filter","query":{"$neq":["ServiceName",""]}}]`, false},
		{"service regex", `[{"type":"filter","query":{"$regex":["ServiceName","api.*"]}}]`, false},
		{"or with an unscoped branch", `[{"type":"filter","query":{"$or":[{"$eq":["ServiceName","api"]},{"$containsWords":["Body","x"]}]}}]`, false},
		{"empty or", `[{"type":"filter","query":{"$or":[]}}]`, false},
		{"not service", `[{"type":"filter","query":{"$not":[{"$eq":["ServiceName","api"]}]}}]`, false},
		{"filter after aggregate", `[{"type":"aggregate","function":{"$count":[]},"as":"n","groupby":{"ServiceName":"s"}},{"type":"filter","query":{"$eq":["ServiceName","api"]}}]`, false},
		{"filter after window aggregate", `[{"type":"window_aggregate","function":{"$count":[]},"as":"n","window":["5","minutes"]},{"type":"filter","query":{"$eq":["ServiceName","api"]}}]`, false},
		{"service name in a non-filter stage", `[{"type":"parse","query":{"$eq":["ServiceName","api"]}}]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PipelineScoped(pipelineFromJSON(t, tt.pipeline)); got != tt.want {
				t.Errorf("PipelineScoped = %v, want %v", got, tt.want)
			}
		})
	}
}

// Tool handlers build pipelines from Go literals whose condition lists are
// []map[string]interface{} rather than the []interface{} JSON decoding yields.
func TestPipelineScoped_GoLiteralConditionLists(t *testing.T) {
	scoped := []map[string]any{{
		"type":  "filter",
		"query": map[string]any{"$and": []map[string]any{{"$eq": []any{"ServiceName", "api"}}}},
	}}
	if !PipelineScoped(scoped) {
		t.Error("[]map[string]any $and with a service eq should be scoped")
	}
	unscoped := []map[string]any{{
		"type":  "filter",
		"query": map[string]any{"$and": []map[string]any{{"$eq": []any{"resources['deployment.environment']", "prod"}}}},
	}}
	if PipelineScoped(unscoped) {
		t.Error("[]map[string]any $and with only an env eq should be unscoped")
	}
}
