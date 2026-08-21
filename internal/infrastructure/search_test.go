package infrastructure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchInfrastructureEntities_HostPage(t *testing.T) {
	var gotQuery string
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != constants.EndpointPromQueryInstant {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		gotQuery, _ = payload["query"].(string)
		_ = json.NewEncoder(w).Encode([]instantPoint{
			{Metric: map[string]string{"nodename": "ip-10-0-1-2", "instance": "10.0.1.2:9100", "job": "node"}},
			{Metric: map[string]string{"nodename": "ip-10-0-1-3", "instance": "10.0.1.3:9100"}},
		})
	}))
	defer srv.Close()

	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
		EntityType: "host",
		Limit:      1,
		Timestamp:  1700000000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != `{__name__=~"node_uname_info|system_uname_info"}` {
		t.Fatalf("promql %q", gotQuery)
	}
	if payload["read_url"] != "https://prom.example/read" {
		t.Fatalf("read_url %+v", payload)
	}
	page := decodeSearchPage(t, result)
	if len(page.Entities) != 1 || page.NextCursor != "1" {
		t.Fatalf("page %+v", page)
	}
	if page.Entities[0].Type != "host" || page.Entities[0].Attributes["host_id"] != "ip-10-0-1-2" {
		t.Fatalf("entity %+v", page.Entities[0])
	}
	if !strings.Contains(page.Entities[0].UI.Href, "/hosts/ip-10-0-1-2") {
		t.Fatalf("href %q", page.Entities[0].UI.Href)
	}
}

func TestSearchInfrastructureEntities_FiltersAndEscapesQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotQuery, _ = payload["query"].(string)
		_ = json.NewEncoder(w).Encode([]instantPoint{
			{Metric: map[string]string{"cluster": "prod", "node": "web-01"}},
			{Metric: map[string]string{"cluster": "prod", "node": "db-01"}},
		})
	}))
	defer srv.Close()

	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
		EntityType: "k8s_node",
		Query:      "web",
		Timestamp:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != `kube_node_info{node=~".*web.*"}` {
		t.Fatalf("promql %q", gotQuery)
	}
	page := decodeSearchPage(t, result)
	if len(page.Entities) != 1 || page.Entities[0].Attributes["node"] != "web-01" {
		t.Fatalf("page %+v", page)
	}
}

func TestSearchPromQL_EscapesRegexMetacharacters(t *testing.T) {
	got := searchPromQL("k8s_node", "web.01")
	want := `kube_node_info{node=~".*web\.01.*"}`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSearchInfrastructureEntities_DedupesClusters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]instantPoint{
			{Metric: map[string]string{"cluster": "prod", "node": "a"}},
			{Metric: map[string]string{"cluster": "prod", "node": "b"}},
			{Metric: map[string]string{"cluster": "stage", "node": "c"}},
		})
	}))
	defer srv.Close()

	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
		EntityType: "k8s_cluster",
		Timestamp:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := decodeSearchPage(t, result)
	if len(page.Entities) != 2 {
		t.Fatalf("want 2 clusters, got %+v", page.Entities)
	}
}

func TestSearchInfrastructureEntities_PodAttributes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]instantPoint{
			{Metric: map[string]string{"cluster": "prod", "namespace": "default", "pod": "api-1", "uid": "uid-1", "node": "n1"}},
		})
	}))
	defer srv.Close()

	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
		EntityType: "k8s_pod",
		Timestamp:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := decodeSearchPage(t, result)
	if len(page.Entities) != 1 {
		t.Fatalf("page %+v", page)
	}
	ent := page.Entities[0]
	if ent.ID != "k8s-pod:cluster-uuid:prod:default:uid-1" {
		t.Fatalf("id %q", ent.ID)
	}
	if ent.Attributes["namespace"] != "default" || ent.Attributes["pod"] != "api-1" {
		t.Fatalf("attrs %+v", ent.Attributes)
	}
}

func TestSearchInfrastructureEntities_RejectsUnknownType(t *testing.T) {
	handler := NewSearchInfrastructureEntitiesHandler(http.DefaultClient, testResolveConfig("http://example.invalid"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{EntityType: "service"})
	if err == nil || !strings.Contains(err.Error(), "unsupported entity_type") {
		t.Fatalf("got %v", err)
	}
}

func TestSearchInfrastructureEntities_InvalidCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]instantPoint{})
	}))
	defer srv.Close()
	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	handler := NewSearchInfrastructureEntitiesHandler(srv.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{
		EntityType: "host",
		Cursor:     "nope",
		Timestamp:  1,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid cursor") {
		t.Fatalf("got %v", err)
	}
}

func TestSearchInfrastructureEntities_RequiresPromURL(t *testing.T) {
	handler := NewSearchInfrastructureEntitiesHandler(http.DefaultClient, testResolveConfig("http://example.invalid"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SearchInfrastructureEntitiesArgs{EntityType: "host"})
	if err == nil || !strings.Contains(err.Error(), "prometheus read URL") {
		t.Fatalf("got %v", err)
	}
}

func decodeSearchPage(t *testing.T, result *mcp.CallToolResult) searchPage {
	t.Helper()
	text := result.Content[0].(*mcp.TextContent).Text
	var page searchPage
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return page
}
