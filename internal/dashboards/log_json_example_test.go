package dashboards

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

var logJSONExample = regexp.MustCompile("Example: `?(\\[\\{[^`]*\\}\\])")

// Agents copy the documented log_json example into get_logs to check a panel; unscoped,
// it returned 177 KB against a 32 KB response bound (#493).
func TestLogJSONExampleIsScopedAndAggregated(t *testing.T) {
	queryProps := queriesArraySchema()["items"].(map[string]interface{})["properties"].(map[string]interface{})
	for name, text := range map[string]string{
		"create_dashboard description": prompts.CreateDashboardDescription,
		"expr schema description":      queryProps["expr"].(map[string]interface{})["description"].(string),
	} {
		t.Run(name, func(t *testing.T) {
			m := logJSONExample.FindStringSubmatch(text)
			if m == nil {
				t.Fatal("no log_json pipeline example found")
			}
			var pipeline []map[string]any
			if err := json.Unmarshal([]byte(m[1]), &pipeline); err != nil {
				t.Fatalf("example is not a JSON pipeline: %v\n%s", err, m[1])
			}
			for _, f := range lintPipeline(m[1], "log_json", "timeseries") {
				if f["severity"] == severityError {
					t.Errorf("example fails panel lint: %v", f)
				}
			}
			if len(pipeline) != 2 || pipeline[0]["type"] != "filter" || pipeline[1]["type"] != "window_aggregate" {
				t.Fatalf("want a filter stage then one window_aggregate, got %s", m[1])
			}
			filter, _ := json.Marshal(pipeline[0]["query"])
			for _, field := range []string{`"ServiceName"`, `"resources['deployment.environment']"`} {
				if !strings.Contains(string(filter), field) {
					t.Errorf("filter does not scope by %s: %s", field, filter)
				}
			}
			agg := pipeline[1]
			if w, _ := json.Marshal(agg["window"]); string(w) != `["1","minutes"]` {
				t.Errorf("window = %s, want a 1-minute bucket", w)
			}
			if agg["groupby"] != nil {
				t.Errorf("groupby %v multiplies the series; the filter already scopes the panel", agg["groupby"])
			}
		})
	}
}

func TestCreateDashboardDescriptionSaysPanelChecksAggregate(t *testing.T) {
	const note = "log panels: aggregate expr only, short lookback"
	if !strings.Contains(prompts.CreateDashboardDescription, note) {
		t.Errorf("CreateDashboardDescription missing %q", note)
	}
}
