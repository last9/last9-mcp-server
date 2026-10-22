package traces

import (
	"strings"
	"testing"
)

func TestSanitizeTraceJSONQuery_RejectsNonRE2Patterns(t *testing.T) {
	cases := []struct {
		name  string
		stage map[string]interface{}
		path  string
	}{
		{
			name: "filter $regex lookahead",
			stage: map[string]interface{}{
				"type":  "filter",
				"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$regex": []interface{}{"SpanName", `^(?!GET /health).*`}}}},
			},
			path: "tracejson_query[0].query.$and[0].$regex[1]",
		},
		{
			name: "where $notregex possessive",
			stage: map[string]interface{}{
				"type":  "where",
				"query": map[string]interface{}{"$notregex": []interface{}{"SpanName", `[0-9]++`}},
			},
			path: "tracejson_query[0].query.$notregex[1]",
		},
		{
			name: "parse regexp lookbehind",
			stage: map[string]interface{}{
				"type":    "parse",
				"parser":  "regexp",
				"pattern": `(?<=id=)(?P<id>\d+)`,
			},
			path: "tracejson_query[0].pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SanitizeTraceJSONQuery([]map[string]interface{}{tc.stage})
			if err == nil {
				t.Fatal("expected a non-RE2 pattern to be rejected before any upstream call")
			}
			for _, want := range []string{tc.path, "RE2"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q missing %q", err, want)
				}
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_AcceptsRE2Patterns(t *testing.T) {
	err := SanitizeTraceJSONQuery([]map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
			map[string]interface{}{"$regex": []interface{}{"SpanName", `^checkout.*`}},
			map[string]interface{}{"$regex": []interface{}{"attributes['http.status_code']", `^[0-9]+(?:\.[0-9]+)?$`}},
		}}},
		{"type": "parse", "parser": "regexp", "pattern": `id=(?P<id>\d+)`},
	})
	if err != nil {
		t.Fatalf("valid RE2 patterns rejected: %v", err)
	}
}
