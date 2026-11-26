package changeimpact

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/traces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type windowData struct {
	requests map[string]float64
	errors   map[string]float64
	p95      map[string]float64
}

// fakeLast9 serves the calls analyze_change_impact makes. Prom instant queries
// are told apart by metric name and by their evaluation timestamp: at or before
// the change is the baseline window, later is the after window.
type fakeLast9 struct {
	change          time.Time
	baseline, after windowData
	callers         map[string]float64
	alerts          []alerting.AlertRuleData
	traceDeviations []traces.AttributeDeviation
	fail            map[string]int // route → status
	failQuery       map[string]int // PromQL substring → status

	mu       sync.Mutex
	requests []string
}

func (f *fakeLast9) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := routeOf(r.URL.Path)
		f.mu.Lock()
		f.requests = append(f.requests, route)
		f.mu.Unlock()
		if status := f.fail[route]; status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch route {
		case "prom":
			var req struct {
				Query     string `json:"query"`
				Timestamp int64  `json:"timestamp"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode prom body: %v", err)
			}
			for substr, status := range f.failQuery {
				if strings.Contains(req.Query, substr) {
					w.WriteHeader(status)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(f.promAnswer(req.Query, req.Timestamp))
		case "alerts":
			_ = json.NewEncoder(w).Encode(alerting.AlertsResponse{AlertRules: f.alerts})
		case "trace_deviations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contract_version": "investigation-evidence/v1",
				"analysis_version": "trace-attribute-deviations/v1",
				"evidence":         map[string]any{"warnings": []string{"candidate_limit_reached"}},
				"data":             map[string]any{"deviations": f.traceDeviations},
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func routeOf(path string) string {
	switch {
	case strings.HasSuffix(path, "/prom_query_instant"):
		return "prom"
	case strings.HasSuffix(path, "/alerts/monitor"):
		return "alerts"
	case strings.HasSuffix(path, "/attribute-deviations"):
		return "trace_deviations"
	}
	return path
}

func (f *fakeLast9) promAnswer(query string, ts int64) []map[string]any {
	w := f.after
	if ts <= f.change.Unix() {
		w = f.baseline
	}
	sample := func(labels map[string]string, v float64) map[string]any {
		return map[string]any{"metric": labels, "value": []any{float64(ts), strconv.FormatFloat(v, 'f', -1, 64)}}
	}
	out := []map[string]any{}
	switch {
	case strings.Contains(query, "trace_call_graph_count"):
		for caller, rpm := range f.callers {
			out = append(out, sample(map[string]string{"client": caller}, rpm))
		}
	case strings.Contains(query, "trace_service_response_time") && strings.Contains(query, "p99"):
		for svc, v := range w.p95 {
			out = append(out, sample(map[string]string{"service_name": svc, "env": "prod", "quantile": "p95"}, v))
		}
	case strings.HasPrefix(query, "sum by (service_name, env)(sum_over_time(trace_endpoint_count{"):
		values := w.requests
		switch {
		case strings.Contains(query, `http_status_code=~"5.*"`):
			values = w.errors
		case strings.Contains(query, "http_status_code"), strings.Contains(query, "grpc_status_code"):
			values = nil
		}
		for svc, v := range values {
			out = append(out, sample(map[string]string{"service_name": svc, "env": "prod"}, v))
		}
	}
	return out
}

func (f *fakeLast9) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == route {
			n++
		}
	}
	return n
}

func testConfig(url string) models.Config {
	return models.Config{
		APIBaseURL: url,
		Region:     "us-east-1",
		OrgSlug:    "acme",
		ClusterID:  "c1",
		TokenManager: &auth.TokenManager{
			AccessToken: "token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

// recentChange is a change two hours ago, so the default after window is complete.
func recentChange() time.Time {
	return time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Hour)
}

func flatWindow() windowData {
	return windowData{
		requests: map[string]float64{"checkout": 1200, "web": 600},
		errors:   map[string]float64{"checkout": 6, "web": 3},
		p95:      map[string]float64{"checkout": 100, "web": 80},
	}
}

func runTool(t *testing.T, f *fakeLast9, args AnalyzeChangeImpactArgs) (Report, error) {
	t.Helper()
	return runToolWith(t, f, args, func(*models.Config) {})
}

func runToolWith(t *testing.T, f *fakeLast9, args AnalyzeChangeImpactArgs, configure func(*models.Config)) (Report, error) {
	t.Helper()
	text, err := runToolText(t, f, args, configure)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, text)
	}
	return report, nil
}

// runToolText returns the report exactly as the tool serves it.
func runToolText(t *testing.T, f *fakeLast9, args AnalyzeChangeImpactArgs, configure func(*models.Config)) (string, error) {
	t.Helper()
	server := f.serve(t)
	defer server.Close()
	cfg := testConfig(server.URL)
	configure(&cfg)
	result, _, err := NewAnalyzeChangeImpactHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", err
	}
	return result.Content[0].(*mcp.TextContent).Text, nil
}

func serviceArgs(change time.Time) AnalyzeChangeImpactArgs {
	return AnalyzeChangeImpactArgs{Service: "checkout", Env: "prod", Timestamp: change.Format(time.RFC3339)}
}

func signalsFor(t *testing.T, r Report, service string) ServiceSignals {
	t.Helper()
	for _, s := range r.GoldenSignals.Services {
		if s.Service == service {
			return s
		}
	}
	t.Fatalf("no golden signals for %q in %+v", service, r.GoldenSignals.Services)
	return ServiceSignals{}
}
