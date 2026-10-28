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
	q := map[string]interface{}{"$eq": []interface{}{"ServiceName", "x"}}
	tests := []struct {
		name  string
		stage map[string]interface{}
	}{
		{"filter query", map[string]interface{}{"type": "filter", "query": q}},
		{"where query", map[string]interface{}{"type": "where", "query": q}},
		{"parse field", map[string]interface{}{"type": "parse", "parser": "json", "field": "Body", "pattern": "(?P<id>.*)", "labels": map[string]interface{}{"user_id": "uid"}}},
		{"transform transforms", map[string]interface{}{"type": "transform", "transforms": []interface{}{map[string]interface{}{"function": map[string]interface{}{"$split": []interface{}{"SpanName", "/", 1}}, "as": "endpoint"}}}},
		{"select order limit offset", map[string]interface{}{"type": "select", "labels": map[string]interface{}{"ServiceName": "service"}, "order": "Duration", "limit": 10, "offset": 1}},
		{"aggregate fields", map[string]interface{}{"type": "aggregate", "aggregates": []interface{}{map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": "count"}}, "groupby": map[string]interface{}{"ServiceName": "service"}}},
		{"window aggregate", map[string]interface{}{"type": "window_aggregate", "function": map[string]interface{}{"$count": []interface{}{}}, "as": "count", "window": []interface{}{5, "m"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := SanitizeTraceJSONQuery([]map[string]interface{}{tt.stage}); err != nil {
				t.Fatalf("valid stage rejected: %v", err)
			}
		})
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
