package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func alertingReferenceURL(t *testing.T, res *mcp.CallToolResult) *url.URL {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res.Content)
	}
	raw, ok := res.Meta["reference_url"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing reference_url in meta: %v", res.Meta)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse reference_url %q: %v", raw, err)
	}
	return u
}

func assertNoAlertingLink(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	if res == nil {
		return
	}
	if _, ok := res.Meta["reference_url"]; ok {
		t.Fatalf("result must not carry a link: %v", res.Meta)
	}
}

func TestGetAlertOperationsSummary_AlertingLink(t *testing.T) {
	f := &opsFake{summary: opsSummaryBody}
	client, cfg := f.serve(t)
	cfg.OrgSlug = "acme"
	res, _, err := NewGetAlertOperationsSummaryHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, weekArgs())
	if err != nil {
		t.Fatal(err)
	}
	u := alertingReferenceURL(t, res)
	if u.Path != "/v2/organizations/acme/alerting/monitor" {
		t.Fatalf("path = %q", u.Path)
	}
	q := u.Query()
	// 2026-10-16T00:00:00Z .. 2026-10-23T00:00:00Z
	if q.Get("from") != "1792108800" || q.Get("to") != "1792713600" {
		t.Fatalf("window = %v", q)
	}
	if q.Has("severity") || q.Has("rule_id") {
		t.Fatalf("unexpected filters: %v", q)
	}
}

func TestGetAlertOperationsSummary_NoLinkOnError(t *testing.T) {
	f := &opsFake{summaryStatus: http.StatusInternalServerError, summary: `{"error":"boom"}`}
	client, cfg := f.serve(t)
	cfg.OrgSlug = "acme"
	res, _, err := NewGetAlertOperationsSummaryHandler(client, cfg)(context.Background(), &mcp.CallToolRequest{}, weekArgs())
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	assertNoAlertingLink(t, res)
}

func TestAlertRuleStateHandler_AlertingGroupsLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timestamp":1718000000,"window":60,"alert_rules":[{"rule_id":"r1","state":"firing"}]}`))
	}))
	defer server.Close()

	handler := NewAlertRuleStateHandler(server.Client(), newAlertRuleStateTestConfig(server.URL))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: ts(1718000000), EndTime: ts(1718000060), Step: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := alertingReferenceURL(t, res).String(); got != "/v2/organizations/test-org/alerting/groups" {
		t.Fatalf("reference_url = %q", got)
	}
}

func TestAlertRuleStateHandler_NoLinkOnError(t *testing.T) {
	handler := NewAlertRuleStateHandler(http.DefaultClient, newAlertRuleStateTestConfig("http://unused"))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{
		StartTime: ts(1718000060), EndTime: ts(1718000000), Step: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	assertNoAlertingLink(t, res)
}

func describeChartLinkServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"signals":[{"key":"p95_latency","unit":"ms","query_kind":"metric"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDescribeAlertChart_ServiceLinkForDiscoverService(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, "2026-02-09T16:04:05Z")
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
	server := describeChartLinkServer(t)
	cfg := testAlertMutationConfig(server.URL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "cluster-1"

	res, _, err := NewDescribeAlertChartHandler(server.Client(), cfg)(context.Background(), nil, DescribeAlertChartArgs{
		Surface: "discover-service", ChartKey: "response_time", ServiceName: "checkout", Env: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	u := alertingReferenceURL(t, res)
	if u.Path != "/v2/organizations/acme/service-catalog/checkout" {
		t.Fatalf("path = %q", u.Path)
	}
	q := u.Query()
	want := map[string]string{
		"from":    "1770649445",
		"to":      "1770653045",
		"cluster": "cluster-1",
		"live":    "true",
		"filter":  `["deployment_environment=\"prod\""]`,
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestDescribeAlertChart_NoLinkForOtherSurfacesOrErrors(t *testing.T) {
	t.Cleanup(resetDescribedChartMetrics)
	server := describeChartLinkServer(t)
	cfg := testAlertMutationConfig(server.URL)
	cfg.OrgSlug = "acme"
	handler := NewDescribeAlertChartHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), nil, DescribeAlertChartArgs{
		Surface: "discover-exceptions", ChartKey: "exception_count", ServiceName: "checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res.Content)
	}
	assertNoAlertingLink(t, res)

	res, _, err = handler(context.Background(), nil, DescribeAlertChartArgs{Surface: "discover-service", ChartKey: "response_time"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error result, got %+v", res)
	}
	assertNoAlertingLink(t, res)
}
