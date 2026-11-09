package logs

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"go.opentelemetry.io/otel/attribute"
)

func TestGetLogsPhaseTiming(t *testing.T) {
	for _, apiMode := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			name := map[bool]string{false: "chunked", true: "search API"}[apiMode] + "/" + map[bool]string{false: "success", true: "failure"}[failed]
			t.Run(name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(5 * time.Millisecond)
					if failed {
						http.Error(w, "unavailable", http.StatusInternalServerError)
						return
					}
					raw := `{"status":"success","data":{"resultType":"streams","result":[]}}`
					if apiMode {
						raw = `{"query_result":` + raw + `}`
					}
					_, _ = w.Write([]byte(raw))
				}))
				defer srv.Close()
				cfg := apiModeCfg(srv.URL)
				cfg.UseLogSearchAPI = apiMode
				ctx, finish := utils.RecordedSpanContext(t)
				_, _, err := NewGetLogsHandler(srv.Client(), cfg)(ctx, nil, GetLogsArgs{
					LogjsonQuery: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}}},
					StartTimeISO: "2026-10-16T10:00:00Z", EndTimeISO: "2026-10-16T10:01:00Z",
				})
				if (err != nil) != failed {
					t.Fatalf("err=%v, want failure %v", err, failed)
				}
				attrs := finish()
				_, sanityTimed := attrs["last9.phase.count_sanity.duration_ms"]
				if sanityTimed != (apiMode && !failed) {
					t.Errorf("count sanity timed=%v, want %v", sanityTimed, apiMode && !failed)
				}
				for _, phase := range []string{"fetch", "post_process", "deep_link"} {
					value, ok := attrs[attribute.Key("last9.phase."+phase+".duration_ms")]
					if failed && phase != "fetch" {
						if ok {
							t.Errorf("unexecuted %s was timed", phase)
						}
						continue
					}
					if !ok || value.Type() != attribute.FLOAT64 || value.AsFloat64() < 0 {
						t.Errorf("missing duration for %s: %v", phase, value)
					}
					if phase == "fetch" && value.AsFloat64() < 5 {
						t.Errorf("fetch duration %v excludes search delay", value)
					}
					if got := attrs[attribute.Key("last9.phase."+phase+".failed")].AsBool(); got != failed {
						t.Errorf("%s failed=%v", phase, got)
					}
				}
			})
		}
	}
}
