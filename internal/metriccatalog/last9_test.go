package metriccatalog

import "testing"

func TestLast9Rollups(t *testing.T) {
	for name, d := range last9Metrics {
		if d.Type != "gauge" || d.Help == "" {
			t.Errorf("%s: %+v", name, d)
		}
		if d.Quantile && d.Unit != "milliseconds" {
			t.Errorf("quantile units: %s: %+v", name, d)
		}
		if got, ok := Last9(name); !ok || got != d {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"trace_unknown", "trace_service_response_time_bucket", "custom_log_count", "last9_change_events_total"} {
		if _, ok := Last9(name); ok {
			t.Errorf("catalog must match exact names: %s", name)
		}
	}
}
