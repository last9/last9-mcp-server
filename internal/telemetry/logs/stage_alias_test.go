package logs

import (
	"encoding/json"
	"strings"
	"testing"
)

func countBy(as string, groupby map[string]interface{}) map[string]interface{} {
	s := aggregateStage(fn1("$count"), as)
	if groupby != nil {
		s["groupby"] = groupby
	}
	return s
}

func windowCountBy(as string, groupby map[string]interface{}) map[string]interface{} {
	s := windowStage(fn1("$count"), as)
	s["window"] = []interface{}{"1", "minutes"}
	if groupby != nil {
		s["groupby"] = groupby
	}
	return s
}

func lastQueryJSON(t *testing.T, stages []map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(stages[len(stages)-1]["query"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPrepareLogJSONQueryAcceptsStageAliases(t *testing.T) {
	scope := cmpFilter("$eq", "ServiceName", "last9-api")
	gb := func(k, v string) map[string]interface{} { return map[string]interface{}{k: v} }
	cases := []struct {
		name   string
		stages []map[string]interface{}
		want   string // empty: only assert no error
	}{
		{"having count", []map[string]interface{}{scope, countBy("n", gb("ServiceName", "svc")), cmpFilter("$gt", "n", "100")}, `{"$and":[{"$gt":["n","100"]}]}`},
		{"groupby alias", []map[string]interface{}{scope, countBy("n", gb("SeverityText", "sev")), cmpFilter("$eq", "sev", "ERROR")}, `{"$and":[{"$eq":["sev","ERROR"]}]}`},
		{"window", []map[string]interface{}{scope, windowCountBy("per_min", gb("ServiceName", "svc")), cmpFilter("$gte", "per_min", 5.0)}, `{"$and":[{"$gte":["per_min","5"]}]}`},
		{"chained", []map[string]interface{}{scope, countBy("n", gb("SeverityText", "sev")), cmpFilter("$gt", "n", "0"),
			{"type": "aggregate", "aggregates": []interface{}{
				map[string]interface{}{"function": fn1("$count"), "as": "levels"},
				map[string]interface{}{"function": fn1("$sum", "n"), "as": "total"},
			}}}, ""},
		{"nested", []map[string]interface{}{scope, countBy("n", gb("ServiceName", "svc")), {"type": "filter", "query": map[string]interface{}{
			"$or": []interface{}{
				map[string]interface{}{"$gt": []interface{}{"n", "1"}},
				map[string]interface{}{"$eq": []interface{}{"svc", "x"}},
			}}}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := prepareLogJSONQuery(c.stages, "logjson_query")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.want != "" {
				if got := lastQueryJSON(t, out); got != c.want {
					t.Fatalf("query = %s, want %s", got, c.want)
				}
			}
		})
	}
}

func TestPrepareLogJSONQueryRejectsOutOfScopeAliases(t *testing.T) {
	scope := cmpFilter("$eq", "ServiceName", "last9-api")
	sev := map[string]interface{}{"SeverityText": "sev"}
	cases := []struct {
		name   string
		stages []map[string]interface{}
		path   string
		substr string
	}{
		{"before aggregate", []map[string]interface{}{scope, cmpFilter("$gt", "n", "100"), countBy("n", sev)}, "logjson_query[1].query.$gt[0]", `invalid log field reference "n"`},
		{"later alias", []map[string]interface{}{scope, countBy("n", sev), cmpFilter("$gt", "m", "1"), countBy("m", nil)}, "logjson_query[2].query.$gt[0]", `"m"`},
		{"unknown name", []map[string]interface{}{scope, countBy("n", sev), cmpFilter("$gt", "count", "1")}, "logjson_query[2].query.$gt[0]", `"count"`},
		{"reset", []map[string]interface{}{scope, countBy("n", sev), countBy("m", nil), cmpFilter("$gt", "n", "1")}, "logjson_query[3].query.$gt[0]", `"n"`},
		{"non numeric", []map[string]interface{}{scope, countBy("n", sev), cmpFilter("$gt", "n", "many")}, "logjson_query[2].query.$gt[1]", "must be a number"},
		{"bad groupby alias", []map[string]interface{}{scope, countBy("n", map[string]interface{}{"ServiceName": "svc name"}), cmpFilter("$eq", "svc name", "x")}, "logjson_query[2].query.$eq[0]", "invalid log field reference"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := prepareLogJSONQuery(c.stages, "logjson_query")
			if err == nil {
				t.Fatal("expected error")
			}
			requireInvalidField(t, err, c.path, c.substr)
		})
	}
}

func TestPrepareLogJSONQueryNotNull(t *testing.T) {
	pass := []struct{ field, want string }{
		{"attributes['user_id']", `{"$and":[{"$notnull":["attributes['user_id']"]}]}`},
		{"service.name", `{"$and":[{"$notnull":["ServiceName"]}]}`},
	}
	for _, c := range pass {
		out, err := prepareLogJSONQuery([]map[string]interface{}{cmpFilter("$notnull", c.field)}, "logjson_query")
		if err != nil {
			t.Fatalf("%s: %v", c.field, err)
		}
		if got := lastQueryJSON(t, out); got != c.want {
			t.Errorf("query = %s, want %s", got, c.want)
		}
	}
	_, err := prepareLogJSONQuery([]map[string]interface{}{cmpFilter("$notnull", "user_id")}, "logjson_query")
	requireInvalidField(t, err, "logjson_query[0].query.$notnull[0]", `invalid log field reference "user_id"`)
	_, err = prepareLogJSONQuery([]map[string]interface{}{cmpFilter("$exists", "Body")}, "logjson_query")
	if err == nil || !strings.Contains(err.Error(), "invalid filter condition key") {
		t.Fatalf("$exists: got %v", err)
	}
}
