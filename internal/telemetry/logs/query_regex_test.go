package logs

import (
	"strings"
	"testing"
)

func TestPrepareLogJSONQueryRejectsNonRE2Patterns(t *testing.T) {
	cases := []struct {
		name  string
		stage map[string]interface{}
		path  string
	}{
		{
			name: "filter $regex lookahead",
			stage: map[string]interface{}{
				"type":  "filter",
				"query": map[string]interface{}{"$and": []interface{}{map[string]interface{}{"$regex": []interface{}{"Body", `^(?!health).*`}}}},
			},
			path: "logjson_query[0].query.$and[0].$regex[1]",
		},
		{
			name: "filter $inotregex lookbehind",
			stage: map[string]interface{}{
				"type":  "filter",
				"query": map[string]interface{}{"$inotregex": []interface{}{"Body", `(?<=user=)\w+`}},
			},
			path: "logjson_query[0].query.$inotregex[1]",
		},
		{
			name: "parse regexp backreference",
			stage: map[string]interface{}{
				"type":    "parse",
				"parser":  "regexp",
				"field":   "Body",
				"pattern": `(?P<word>\w+)\s+\1`,
			},
			path: "logjson_query[0].pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareLogJSONQuery([]map[string]interface{}{tc.stage}, "")
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

func TestPrepareLogJSONQueryAcceptsRE2Patterns(t *testing.T) {
	_, err := prepareLogJSONQuery([]map[string]interface{}{
		{"type": "parse", "parser": "regexp", "field": "Body", "pattern": `status=(?P<status>\d{3})`},
		{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
			map[string]interface{}{"$regex": []interface{}{"attributes['status']", `^5[0-9]{2}$`}},
			map[string]interface{}{"$iregex": []interface{}{"Body", `(?i)timeout|refused`}},
		}}},
	}, "")
	if err != nil {
		t.Fatalf("valid RE2 patterns rejected: %v", err)
	}
}
