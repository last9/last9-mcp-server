package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func tracesLinkServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func tracesReferenceURL(t *testing.T, res *mcp.CallToolResult) *url.URL {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	raw, ok := res.Meta["reference_url"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing reference_url in meta: %v", res.Meta)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse reference_url %q: %v", raw, err)
	}
	if u.Path != "/v2/organizations/acme/traces" {
		t.Fatalf("path = %q, want /v2/organizations/acme/traces", u.Path)
	}
	return u
}

func TestGetTraceAttributeValuesHandler_TracesLink(t *testing.T) {
	start := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Minute)
	server := tracesLinkServer(t, http.StatusOK, `{"status":"success","data":["GET"]}`)
	cfg := tracesTestConfig(server.URL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "cluster-1"
	handler := NewGetTraceAttributeValuesHandler(server.Client(), cfg)

	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
			map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}},
		}}},
	}
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{
		TagName:      "http.method",
		Pipeline:     pipeline,
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	q := tracesReferenceURL(t, res).Query()
	if q.Get("from") != "1770631200" || q.Get("to") != "1770633900" || q.Get("cluster") != "cluster-1" {
		t.Fatalf("unexpected window/cluster: %v", q)
	}
	var got, want any
	if err := json.Unmarshal([]byte(q.Get("pipeline")), &got); err != nil {
		t.Fatalf("pipeline param %q: %v", q.Get("pipeline"), err)
	}
	wantJSON, _ := json.Marshal(pipeline)
	_ = json.Unmarshal(wantJSON, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pipeline = %v, want %v", got, want)
	}
}

func TestGetTraceAttributeValuesHandler_TracesLinkWithoutPipeline(t *testing.T) {
	server := tracesLinkServer(t, http.StatusOK, `{"status":"success","data":["GET"]}`)
	cfg := tracesTestConfig(server.URL)
	cfg.OrgSlug = "acme"
	handler := NewGetTraceAttributeValuesHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if q := tracesReferenceURL(t, res).Query(); q.Has("pipeline") {
		t.Fatalf("pipeline param must be absent without a pipeline: %v", q)
	}
}

func TestGetTraceAttributeValuesHandler_NoLinkOnError(t *testing.T) {
	server := tracesLinkServer(t, http.StatusOK, `{"status":"error","data":null}`)
	cfg := tracesTestConfig(server.URL)
	cfg.OrgSlug = "acme"
	handler := NewGetTraceAttributeValuesHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeValuesArgs{TagName: "http.method"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	if _, ok := res.Meta["reference_url"]; ok {
		t.Fatalf("error result must not carry a link: %v", res.Meta)
	}
}

func TestGetTraceAttributeDeviationsHandler_TracesLink(t *testing.T) {
	start := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)
	server := tracesLinkServer(t, http.StatusOK, `{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`)
	cfg := tracesTestConfig(server.URL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "cluster-1"
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors",
		ServiceName:    "checkout",
		Environment:    "prod",
		Operation:      "GET /cart",
		Filters: []map[string]interface{}{
			{"$eq": []interface{}{"attributes['http.method']", "GET"}},
		},
		StartTimeISO: start.Format(time.RFC3339),
		EndTimeISO:   end.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	q := tracesReferenceURL(t, res).Query()
	if q.Get("from") != "1770631200" || q.Get("to") != "1770632100" || q.Get("cluster") != "cluster-1" {
		t.Fatalf("unexpected window/cluster: %v", q)
	}
	var got any
	if err := json.Unmarshal([]byte(q.Get("pipeline")), &got); err != nil {
		t.Fatalf("pipeline param %q: %v", q.Get("pipeline"), err)
	}
	var want any
	_ = json.Unmarshal([]byte(`[{"type":"filter","query":{"$and":[
		{"$eq":["ServiceName","checkout"]},
		{"$eq":["resources['deployment.environment']","prod"]},
		{"$eq":["SpanName","GET /cart"]},
		{"$eq":["attributes['http.method']","GET"]}
	]}}]`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pipeline = %v, want %v", got, want)
	}
}

func TestGetTraceAttributeDeviationsHandler_NoLinkOnError(t *testing.T) {
	server := tracesLinkServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	cfg := tracesTestConfig(server.URL)
	cfg.OrgSlug = "acme"
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "checkout", Environment: "prod",
	})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res != nil {
		t.Fatalf("error must not return a result with a link: %+v", res)
	}
}
