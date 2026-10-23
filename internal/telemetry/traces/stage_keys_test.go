package traces

import (
	"strings"
	"testing"
)

func TestTraceStageKeyAllowlist_Rejects(t *testing.T) {
	q := map[string]interface{}{"$eq": []interface{}{"ServiceName", "x"}}
	tests := []struct {
		name  string
		stage map[string]interface{}
		key   string
	}{
		{"filter sql", map[string]interface{}{"type": "filter", "query": q, "sql": "1=1"}, "sql"},
		{"filter table", map[string]interface{}{"type": "filter", "query": q, "table": "t"}, "table"},
		{"where sql", map[string]interface{}{"type": "where", "query": q, "sql": "1=1"}, "sql"},
		{"select starttime", map[string]interface{}{"type": "select", "starttime": 1}, "starttime"},
		{"select endtime", map[string]interface{}{"type": "select", "endtime": 1}, "endtime"},
		{"select direction", map[string]interface{}{"type": "select", "direction": "asc"}, "direction"},
		{"aggregate order_by", map[string]interface{}{"type": "aggregate", "order_by": "x"}, "order_by"},
		{"parse delim", map[string]interface{}{"type": "parse", "parser": "json", "delim": ","}, "delim"},
		{"transform sql", map[string]interface{}{"type": "transform", "sql": "x"}, "sql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery([]map[string]interface{}{tt.stage})
			if err == nil || !strings.Contains(err.Error(), `unknown key "`+tt.key+`"`) {
				t.Fatalf("want unknown key %q error, got %v", tt.key, err)
			}
			if !strings.Contains(err.Error(), "allowed keys:") {
				t.Fatalf("missing allowed list: %v", err)
			}
		})
	}
}

func TestTraceStageKeyAllowlist_AllowsDeclaredKeys(t *testing.T) {
	for stageType, allowed := range traceAllowedStageKeys {
		stage := map[string]interface{}{}
		for k := range allowed {
			stage[k] = nil
		}
		stage["type"] = stageType
		if err := validateTraceStageKeyAllowlist(stage, stageType, "p"); err != nil {
			t.Errorf("%s: declared keys rejected: %v", stageType, err)
		}
	}
}

func TestSanitizeTraceJSONQuery_StageKeyAllowlistEndToEnd(t *testing.T) {
	q := map[string]interface{}{"$eq": []interface{}{"ServiceName", "x"}}
	if err := SanitizeTraceJSONQuery([]map[string]interface{}{{"type": "filter", "query": q}}); err != nil {
		t.Fatalf("clean filter rejected: %v", err)
	}
	err := SanitizeTraceJSONQuery([]map[string]interface{}{{"type": "filter", "query": q, "sql": "1=1"}})
	if err == nil || !strings.Contains(err.Error(), `unknown key "sql"`) {
		t.Fatalf("want unknown key sql, got %v", err)
	}
}
