package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const recipeFixture = "../last9api/servicerecipes/testdata/discover_services_overview.json"

type recipeAPI struct {
	calls atomic.Int32
	cfg   models.Config
	srv   *httptest.Server
}

func newRecipeAPI(t *testing.T, status int, body []byte) *recipeAPI {
	t.Helper()
	api := &recipeAPI{}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/recipes/metrics" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(api.srv.Close)
	api.cfg = models.Config{APIBaseURL: api.srv.URL, TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
	return api
}

func newFixtureRecipeAPI(t *testing.T) *recipeAPI {
	t.Helper()
	body, err := os.ReadFile(recipeFixture)
	if err != nil {
		t.Fatal(err)
	}
	return newRecipeAPI(t, http.StatusOK, body)
}

func describeMetric(t *testing.T, api *recipeAPI, metric string) (map[string]any, error) {
	t.Helper()
	handler := NewDescribeMetricHandler(api.srv.Client(), api.cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DescribeMetricArgs{Metric: metric})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func assertAbsent(t *testing.T, out map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := out[k]; ok {
			t.Errorf("%s present in %v", k, out)
		}
	}
}

func TestDescribeMetricServesDiscoverServicesRecipes(t *testing.T) {
	api := newFixtureRecipeAPI(t)

	tp, err := describeMetric(t, api, "trace_endpoint_count")
	if err != nil {
		t.Fatal(err)
	}
	if tp["source"] != "service_recipe" || tp["unit"] != "rpm" || tp["indicator"] != "throughput" {
		t.Fatalf("throughput = %v", tp)
	}
	if tp["recommended_query"] != "(sum(trace_endpoint_count{span_kind='SPAN_KIND_SERVER'})) default 0" {
		t.Fatalf("throughput recommended_query = %v", tp["recommended_query"])
	}
	tpQueries, _ := tp["queries"].(map[string]any)
	tpChart, _ := tpQueries["chart"].(map[string]any)
	tpList, _ := tpQueries["list"].(map[string]any)
	if tpChart["rollup"] != "none" || tpList["rollup"] != "quantile_over_time(0.95)" {
		t.Fatalf("throughput queries = %v", tpQueries)
	}
	if filters, _ := json.Marshal(tp["filters"]); string(filters) != `["span_kind='SPAN_KIND_SERVER'"]` {
		t.Fatalf("throughput filters = %s", filters)
	}
	forbidden, _ := json.Marshal(tp["forbidden_functions"])
	if !strings.Contains(string(forbidden), `"rate"`) || !strings.Contains(string(forbidden), `"histogram_quantile"`) {
		t.Fatalf("forbidden_functions = %s", forbidden)
	}
	assertAbsent(t, tp, "example_query", "counter_rule_query", "basis")

	p95, err := describeMetric(t, api, "trace_service_response_time")
	if err != nil {
		t.Fatal(err)
	}
	const p95Query = "sum by (quantile)(trace_service_response_time{quantile='p95'})"
	queries, _ := p95["queries"].(map[string]any)
	chart, _ := queries["chart"].(map[string]any)
	if p95["source"] != "service_recipe" || p95["unit"] != "ms" || p95["recommended_query"] != p95Query ||
		chart["promql"] != p95Query || p95["quantile_label"] != "p95" {
		t.Fatalf("p95 = %v", p95)
	}
	list, _ := queries["list"].(map[string]any)
	if promql, _ := list["promql"].(string); !strings.Contains(promql, "[{duration}m]") || !strings.Contains(promql, "service_name") {
		t.Fatalf("p95 list query = %v", list)
	}
	assertAbsent(t, p95, "example_query", "counter_rule_query", "basis")
}

func TestDescribeMetricFallsBackToLegacyHeuristicWhenReadFails(t *testing.T) {
	api := newRecipeAPI(t, http.StatusBadGateway, []byte("upstream detail"))

	tp, err := describeMetric(t, api, "trace_endpoint_count")
	if err != nil {
		t.Fatal(err)
	}
	if tp["source"] != "legacy_heuristic" || tp["unit"] != "req/s" ||
		tp["example_query"] != `sum(sum_over_time(trace_endpoint_count{span_kind="SPAN_KIND_SERVER"}[5m])) / 300` ||
		tp["counter_rule_query"] != "sum(rate(trace_endpoint_count[5m]))" {
		t.Fatalf("throughput fallback = %v", tp)
	}
	if basis, _ := tp["basis"].(string); !strings.Contains(basis, "not the Discover Services dashboard query") {
		t.Fatalf("basis = %q", basis)
	}
	assertAbsent(t, tp, "recommended_query", "queries", "filters", "forbidden_functions")

	p95, err := describeMetric(t, api, "trace_service_response_time")
	if err != nil {
		t.Fatal(err)
	}
	if p95["source"] != "legacy_heuristic" || p95["unit"] != "seconds" ||
		p95["example_query"] != "histogram_quantile(0.95, sum by (le) (rate(trace_service_response_time_bucket[5m])))" {
		t.Fatalf("p95 fallback = %v", p95)
	}
	assertAbsent(t, p95, "recommended_query", "counter_rule_query")

	other, err := describeMetric(t, api, "trace_service_apdex_score")
	if err != nil {
		t.Fatal(err)
	}
	if other["source"] != "uncovered" {
		t.Fatalf("other trace metric on failed read = %v", other)
	}
	assertAbsent(t, other, "recommended_query", "example_query", "basis")
	for _, out := range []map[string]any{tp, p95, other} {
		if raw, _ := json.Marshal(out); strings.Contains(string(raw), "upstream detail") {
			t.Fatalf("result leaks upstream body: %s", raw)
		}
	}
}

// The catalog always lists the overview recipes, so a 2xx without any is a broken read, never
// "no recipe for this metric".
func TestDescribeMetricTreatsCatalogLessBodyAsReadFailure(t *testing.T) {
	for _, body := range []string{"", "null", "{}", `{"recipes":null}`, `{"recipes":[]}`} {
		t.Run(body, func(t *testing.T) {
			api := newRecipeAPI(t, http.StatusOK, []byte(body))
			out, err := describeMetric(t, api, "trace_endpoint_count")
			if err != nil {
				t.Fatal(err)
			}
			note, _ := out["note"].(string)
			if out["source"] != "legacy_heuristic" || !strings.Contains(note, "no recipe catalog") {
				t.Fatalf("got %v", out)
			}
		})
	}
}

func TestDescribeMetricTreatsUndecodableBodyAsReadFailure(t *testing.T) {
	const marker = "QA_MARKER_7f3a"
	bodies := map[string]string{
		"malformed JSON": `{"recipes": [` + marker,
		// The shape guessed before the backend golden: queries as bare strings.
		"string queries": `{"recipes":[{"metric":"trace_endpoint_count","recommended_query":"q",` +
			`"queries":{"chart":"` + marker + `","list":"` + marker + `"}}]}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			api := newRecipeAPI(t, http.StatusOK, []byte(body))
			out, err := describeMetric(t, api, "trace_endpoint_count")
			if err != nil {
				t.Fatal(err)
			}
			note, _ := out["note"].(string)
			raw, _ := json.Marshal(out)
			if out["source"] != "legacy_heuristic" || !strings.Contains(note, "failed") || strings.Contains(string(raw), marker) {
				t.Fatalf("got %s", raw)
			}
		})
	}
}

// The tool takes only a metric name, so the client's generic "narrow the request" advice is
// an action the agent cannot take.
func TestDescribeMetricOversizedCatalogNoteOffersNoNarrowing(t *testing.T) {
	body := []byte(`{"recipes":[]}` + strings.Repeat(" ", 1<<20))
	api := newRecipeAPI(t, http.StatusOK, body)

	out, err := describeMetric(t, api, "trace_endpoint_count")
	if err != nil {
		t.Fatal(err)
	}
	note, _ := out["note"].(string)
	if out["source"] != "legacy_heuristic" || !strings.Contains(note, "exceeds 1048576 bytes") || strings.Contains(note, "narrow") {
		t.Fatalf("oversized catalog = %v", out)
	}
}

// The shared client relays 400/422 bodies and asks the agent to fix its arguments, but this
// tool's request carries no argument the agent could fix.
func TestDescribeMetricReadFailureNoteNeverRelaysUpstreamBody(t *testing.T) {
	const marker = "LEAK-MARKER"
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusBadGateway} {
		api := newRecipeAPI(t, status, []byte(`{"error":"bad `+marker+`"}`))
		for _, metric := range []string{"trace_endpoint_count", "trace_service_apdex_score"} {
			out, err := describeMetric(t, api, metric)
			if err != nil {
				t.Fatalf("HTTP %d %s: %v", status, metric, err)
			}
			raw, _ := json.Marshal(out)
			note, _ := out["note"].(string)
			if strings.Contains(string(raw), marker) || strings.Contains(note, "tool arguments") ||
				!strings.Contains(note, fmt.Sprintf("HTTP %d", status)) {
				t.Errorf("HTTP %d %s: %s", status, metric, raw)
			}
		}
	}
}

func TestDescribeMetricUncovered(t *testing.T) {
	t.Run("non trace metric makes no call", func(t *testing.T) {
		api := newFixtureRecipeAPI(t)
		out, err := describeMetric(t, api, "http_requests_total")
		if err != nil {
			t.Fatal(err)
		}
		if out["source"] != "uncovered" || api.calls.Load() != 0 {
			t.Fatalf("got %v after %d calls", out, api.calls.Load())
		}
		assertAbsent(t, out, "recommended_query", "example_query")
	})
	t.Run("trace metric missing from catalog", func(t *testing.T) {
		api := newFixtureRecipeAPI(t)
		out, err := describeMetric(t, api, "trace_service_apdex_score")
		if err != nil {
			t.Fatal(err)
		}
		if out["source"] != "uncovered" || api.calls.Load() != 1 {
			t.Fatalf("got %v after %d calls", out, api.calls.Load())
		}
		assertAbsent(t, out, "recommended_query", "example_query", "queries")
	})
}

func TestDescribeMetricRejectsDuplicateRecipes(t *testing.T) {
	recipe := `{"metric":"trace_endpoint_count","unit":"rpm","recommended_query":"q"}`
	api := newRecipeAPI(t, http.StatusOK, []byte(`{"recipes":[`+recipe+`,`+recipe+`]}`))
	if _, err := describeMetric(t, api, "trace_endpoint_count"); err == nil || !strings.Contains(err.Error(), "2 recipes") {
		t.Fatalf("err = %v, want a duplicate-recipe error", err)
	}
}

func TestDescribeMetricRejectsRecipeWithoutRecommendedQuery(t *testing.T) {
	for name, recipe := range map[string]string{
		"absent": `{"metric":"trace_endpoint_count","unit":"rpm"}`,
		"blank":  `{"metric":"trace_endpoint_count","unit":"rpm","recommended_query":"  "}`,
	} {
		t.Run(name, func(t *testing.T) {
			api := newRecipeAPI(t, http.StatusOK, []byte(`{"recipes":[`+recipe+`]}`))
			if _, err := describeMetric(t, api, "trace_endpoint_count"); err == nil || !strings.Contains(err.Error(), "no recommended_query") {
				t.Fatalf("err = %v, want a missing recommended_query error", err)
			}
		})
	}
}

func TestDescribeMetricRejectsNonBareNames(t *testing.T) {
	for _, metric := range []string{"", "  ", `trace_endpoint_count{span_kind="SPAN_KIND_SERVER"}`, "sum(trace_endpoint_count)", "trace_endpoint_count[5m]", "trace endpoint"} {
		t.Run(metric, func(t *testing.T) {
			api := newFixtureRecipeAPI(t)
			if _, err := describeMetric(t, api, metric); err == nil {
				t.Fatal("want an error")
			}
			if api.calls.Load() != 0 {
				t.Fatalf("made %d API calls for an invalid name", api.calls.Load())
			}
		})
	}
}
