package changeimpact

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// apmRollupServer answers get_apm_service_deviations' service rollup queries
// (the ones evaluated with "@") with flat request and error aggregates, failing
// every Apdex query with 503 when failApdex is set. Everything else falls
// through to f.
func apmRollupServer(t *testing.T, f *fakeLast9, failApdex bool, rejected *atomic.Int32) *httptest.Server {
	t.Helper()
	fallback := f.serve(t)
	t.Cleanup(fallback.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q struct {
			Query     string `json:"query"`
			Timestamp int64  `json:"timestamp"`
		}
		_ = json.Unmarshal(body, &q)
		if !strings.Contains(q.Query, " @ ") {
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			fallback.Config.Handler.ServeHTTP(w, r)
			return
		}
		switch {
		case strings.Contains(q.Query, "apdex") && failApdex:
			rejected.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case strings.Contains(q.Query, "apdex"), strings.Contains(q.Query, "trace_service_response_time"), strings.Contains(q.Query, "deviation_stat"):
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		value := 1200.0
		if strings.Contains(q.Query, "status_code") {
			value = 6
		}
		if strings.HasPrefix(q.Query, "count_over_time") {
			value = 60
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{
			"metric": map[string]string{"service_name": "checkout", "env": "prod"},
			"value":  []any{float64(q.Timestamp), strconv.FormatFloat(value, 'f', -1, 64)},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnalyzeAPMPartialFailureCannotClearChange(t *testing.T) {
	change := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)
	for _, failApdex := range []bool{false, true} {
		t.Run("apdex failed="+strconv.FormatBool(failApdex), func(t *testing.T) {
			var rejected atomic.Int32
			f := &fakeLast9{change: change, baseline: flatWindow(), after: flatWindow()}
			srv := apmRollupServer(t, f, failApdex, &rejected)
			report := Analyze(context.Background(), srv.Client(), testConfig(srv.URL), Request{Service: "checkout", Env: "prod", Change: change, Now: change.Add(2 * time.Hour), BaselineMinutes: 60, AfterMinutes: 60})
			if failApdex && rejected.Load() == 0 {
				t.Fatal("failure injection not reached")
			}
			want := VerdictNoSignificantChange
			if failApdex {
				want = VerdictInsufficientData
			}
			if report.Verdict != want {
				t.Fatalf("verdict=%q want=%q APM=%+v reason=%q", report.Verdict, want, report.Deviations.APM, report.Reason)
			}
			if !failApdex {
				if len(report.Deviations.APM.Unavailable) != 0 {
					t.Fatalf("healthy APM section lists unavailable signals: %v", report.Deviations.APM.Unavailable)
				}
				return
			}
			if !strings.Contains(report.Reason, "APM deviations partially unavailable") || !strings.Contains(report.Reason, "apdex") {
				t.Fatalf("reason does not name the failed APM signals: %q", report.Reason)
			}
			for _, item := range report.Deviations.APM.Unavailable {
				if !strings.Contains(item, "apdex") {
					t.Fatalf("unavailable lists a signal that succeeded: %v", report.Deviations.APM.Unavailable)
				}
			}
			if len(report.Deviations.APM.Unavailable) == 0 {
				t.Fatal("APM section does not list the failed signals")
			}
		})
	}
}

func TestAnalyzeAPMPartialFailureKeepsEstablishedRegression(t *testing.T) {
	change := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)
	after := flatWindow()
	after.errors = map[string]float64{"checkout": 120, "web": 3}
	var rejected atomic.Int32
	f := &fakeLast9{change: change, baseline: flatWindow(), after: after}
	srv := apmRollupServer(t, f, true, &rejected)
	report := Analyze(context.Background(), srv.Client(), testConfig(srv.URL), Request{Service: "checkout", Env: "prod", Change: change, Now: change.Add(2 * time.Hour), BaselineMinutes: 60, AfterMinutes: 60})
	if report.Verdict != VerdictRegressionLikely {
		t.Fatalf("verdict=%q want=%q reason=%q", report.Verdict, VerdictRegressionLikely, report.Reason)
	}
	if !strings.Contains(report.Reason, "error rate rose") || !strings.Contains(report.Reason, "APM deviations partially unavailable") {
		t.Fatalf("reason must keep the regression and name the missing APM evidence: %q", report.Reason)
	}
}
