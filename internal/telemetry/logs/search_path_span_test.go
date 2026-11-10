package logs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"last9-mcp/internal/utils"
)

const (
	attrSearchPath     = attribute.Key("last9.logs.search_path")
	attrSearchStrategy = attribute.Key("last9.logs.search_strategy")
	attrUpstreamStatus = attribute.Key("last9.logs.upstream_status")
)

func statusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertSpanAttr(t *testing.T, attrs map[attribute.Key]attribute.Value, key attribute.Key, want attribute.Value) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Errorf("span has no %s, want %v", key, want.Emit())
		return
	}
	if got != want {
		t.Errorf("span %s = %v, want %v", key, got.Emit(), want.Emit())
	}
}

const searchBodyWithStrategy = `{
	"query_result":{"status":"success","data":{"resultType":"streams","result":[]}},
	"search_stats":{"strategy":"probe_then_fetch"}
}`

const chunkBody = `{"status":"success","data":{"resultType":"streams","result":[]}}`

func TestGetLogsSpan_LogSearchAPIPath(t *testing.T) {
	srv := statusServer(t, http.StatusOK, searchBodyWithStrategy)
	ctx, ended := utils.RecordedSpanContext(t)

	if _, err := fetchLogJSONQuery(ctx, srv.Client(), apiModeCfg(srv.URL),
		[]map[string]interface{}{{"type": "filter"}},
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{}); err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("log_search_api"))
	assertSpanAttr(t, attrs, attrSearchStrategy, attribute.StringValue("probe_then_fetch"))
	assertSpanAttr(t, attrs, attrUpstreamStatus, attribute.IntValue(http.StatusOK))
}

func TestGetLogsSpan_LogSearchAPIUpstreamError(t *testing.T) {
	srv := statusServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	ctx, ended := utils.RecordedSpanContext(t)

	if _, err := fetchLogJSONQuery(ctx, srv.Client(), apiModeCfg(srv.URL),
		[]map[string]interface{}{{"type": "filter"}},
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{}); err == nil {
		t.Fatal("a 500 must fail the call")
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("log_search_api"))
	assertSpanAttr(t, attrs, attrUpstreamStatus, attribute.IntValue(http.StatusInternalServerError))
	if _, ok := attrs[attrSearchStrategy]; ok {
		t.Error("a failed search has no search_stats, so no strategy attribute")
	}
}

func TestGetLogsSpan_ChunkedPath(t *testing.T) {
	srv := statusServer(t, http.StatusOK, chunkBody)
	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false
	ctx, ended := utils.RecordedSpanContext(t)

	if _, err := fetchLogJSONQuery(ctx, srv.Client(), cfg,
		[]map[string]interface{}{{"type": "filter"}},
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{}); err != nil {
		t.Fatalf("fetchLogJSONQuery: %v", err)
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("chunked"))
	assertSpanAttr(t, attrs, attrUpstreamStatus, attribute.IntValue(http.StatusOK))
}

func TestGetLogsSpan_ChunkedUpstreamError(t *testing.T) {
	srv := statusServer(t, http.StatusBadGateway, `{"error":"bad gateway"}`)
	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false
	ctx, ended := utils.RecordedSpanContext(t)

	if _, err := fetchLogJSONQuery(ctx, srv.Client(), cfg,
		[]map[string]interface{}{{"type": "filter"}},
		1_700_000_000_000, 1_700_000_600_000, GetLogsArgs{}); err == nil {
		t.Fatal("a 502 on every chunk must fail the call")
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("chunked"))
	assertSpanAttr(t, attrs, attrUpstreamStatus, attribute.IntValue(http.StatusBadGateway))
}

func TestServiceLogsSpan_LogSearchAPIPath(t *testing.T) {
	srv := statusServer(t, http.StatusOK, searchBodyWithStrategy)
	ctx, ended := utils.RecordedSpanContext(t)

	start, end := serviceLogsWindow()
	if _, err := fetchServiceLogs(ctx, srv.Client(), apiModeCfg(srv.URL), "checkout",
		start, end, 20, serviceLogsPipeline(), ""); err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("log_search_api"))
	assertSpanAttr(t, attrs, attrSearchStrategy, attribute.StringValue("probe_then_fetch"))
}

func TestServiceLogsSpan_ChunkedPath(t *testing.T) {
	srv := statusServer(t, http.StatusOK, chunkBody)
	cfg := apiModeCfg(srv.URL)
	cfg.UseLogSearchAPI = false
	ctx, ended := utils.RecordedSpanContext(t)

	start, end := serviceLogsWindow()
	if _, err := fetchServiceLogs(ctx, srv.Client(), cfg, "checkout",
		start, end, 20, serviceLogsPipeline(), ""); err != nil {
		t.Fatalf("fetchServiceLogs: %v", err)
	}

	attrs := ended()
	assertSpanAttr(t, attrs, attrSearchPath, attribute.StringValue("chunked"))
	assertSpanAttr(t, attrs, attrUpstreamStatus, attribute.IntValue(http.StatusOK))
}
