package nameresolution

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEnvironmentMatchers(t *testing.T) {
	for _, tt := range []struct {
		op, field, value, want string
		ok                     bool
	}{
		{"$eq", "resources['deployment.environment']", "prod", `env="prod"`, true},
		{"$ieq", "resources['deployment.environment.name']", "prod.blue", `env=~"(?i)prod\\.blue"`, true},
		{"$regex", "resources['deployment.environment']", "prod.*", "", false},
		{"$neq", "resources['deployment.environment']", "prod", "", false},
	} {
		t.Run(tt.op, func(t *testing.T) {
			pipeline := []map[string]any{{"type": "filter", "query": map[string]any{"$and": []any{map[string]any{"$eq": []any{"ServiceName", "checkuot"}}, map[string]any{tt.op: []any{tt.field, tt.value}}}}}}
			p, ok := PrepareServicePipeline(pipeline)
			if !ok {
				t.Fatal("pipeline rejected")
			}
			got, ok := p.EnvironmentMatchers()
			if ok != tt.ok || (tt.ok && !reflect.DeepEqual(got, []string{tt.want})) {
				t.Fatalf("matchers=%v ok=%v", got, ok)
			}
		})
	}
}

func TestVerifiedEmpty(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{`{"status":"success","data":{"result":[]}}`, true},
		{`{"status":"error","data":{"result":[]}}`, false},
		{`{"status":"success","data":{"result":[]},"l9_result":{"partial":true}}`, false},
		{`{"status":"success","data":{"result":[]},"total_matching_lines":4}`, false},
		{`{"data":{"result":[]}}`, false},
		{`{"status":"success","data":{"result":null}}`, false},
		{`{"status":"success","data":{"result":[{}]}}`, false},
		{`{"status":"success","data":{"result":[]},"partial":true}`, false},
		{`{"status":"success","data":{"result":[]},"warnings":["partial"]}`, false},
		{`{"status":"success","data":{"result":[]},"search_stats":{"chunks_failed":1}}`, false},
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(tt.raw), &value); err != nil {
			t.Fatal(err)
		}
		if got := VerifiedEmpty(value); got != tt.want {
			t.Errorf("VerifiedEmpty(%s)=%v", tt.raw, got)
		}
	}
}

func TestVerifiedEmptyRawTraceFields(t *testing.T) {
	value := map[string]any{"status": json.RawMessage(`"success"`), "data": map[string]any{"result": []json.RawMessage{}}}
	if !VerifiedEmpty(value) {
		t.Fatal("raw trace status not recognized")
	}
	value["warnings"] = json.RawMessage(`["partial"]`)
	if VerifiedEmpty(value) {
		t.Fatal("raw trace warning ignored")
	}
}
