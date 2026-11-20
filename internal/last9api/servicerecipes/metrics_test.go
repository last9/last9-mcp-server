package servicerecipes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func testClient(t *testing.T, h http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := models.Config{APIBaseURL: srv.URL, TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}}
	return last9api.NewClient(srv.Client(), cfg)
}

func TestListMetricRecipesDecodesCatalog(t *testing.T) {
	fixture, err := os.ReadFile("testdata/discover_services_overview.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/recipes/metrics" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write(fixture)
	})

	got, err := ListMetricRecipes(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d recipes, want 2", len(got))
	}
	tp := got[0]
	if tp.Metric != "trace_endpoint_count" || tp.Indicator != "throughput" || tp.Unit != "rpm" {
		t.Fatalf("unexpected throughput recipe: %+v", tp)
	}
	if tp.RecommendedQuery != "(sum(trace_endpoint_count{span_kind='SPAN_KIND_SERVER'})) default 0" {
		t.Fatalf("recommended_query = %q", tp.RecommendedQuery)
	}
	if tp.Queries.Chart.Rollup != "none" || tp.Queries.List.Rollup != "quantile_over_time(0.95)" {
		t.Fatalf("query rollups = %+v", tp.Queries)
	}
	if !strings.Contains(tp.Queries.List.PromQL, "[{duration}m]") || !strings.HasPrefix(tp.Queries.Chart.PromQL, "((sum by (service_name, http_status_code)") {
		t.Fatalf("queries = %+v", tp.Queries)
	}
	if strings.Join(tp.Filters, ",") != "span_kind='SPAN_KIND_SERVER'" || tp.QuantileLabel != "" {
		t.Fatalf("filters = %v, quantile_label = %q", tp.Filters, tp.QuantileLabel)
	}
	if strings.Join(tp.ForbiddenFunctions, ",") != "rate,irate,increase,histogram_quantile" {
		t.Fatalf("forbidden_functions = %v", tp.ForbiddenFunctions)
	}
	if p95 := got[1]; p95.QuantileLabel != "p95" || strings.Join(p95.Filters, ",") != "quantile='p95'" {
		t.Fatalf("p95 filters = %v, quantile_label = %q", p95.Filters, p95.QuantileLabel)
	}
}

func TestListMetricRecipesRejectsBodiesWithoutCatalog(t *testing.T) {
	for _, body := range []string{"", "null", "{}", `{"recipes":null}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := ListMetricRecipes(context.Background(), c); err == nil {
				t.Fatal("want an error for a body without a recipe catalog")
			}
		})
	}
}

func TestListMetricRecipesAcceptsEmptyCatalog(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"recipes":[]}`)) })
	got, err := ListMetricRecipes(context.Background(), c)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want an empty catalog", got, err)
	}
}

func TestListMetricRecipesSanitizesErrors(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal-detail-leak", http.StatusBadGateway)
	})
	_, err := ListMetricRecipes(context.Background(), c)
	if err == nil {
		t.Fatal("want an error on 502")
	}
	if strings.Contains(err.Error(), "internal-detail-leak") {
		t.Fatalf("error leaks upstream body: %v", err)
	}
}
