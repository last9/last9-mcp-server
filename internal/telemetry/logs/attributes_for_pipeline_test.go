package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func decodeLogAttributes(t *testing.T, res *mcp.CallToolResult) []LogAttribute {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var attrs []LogAttribute
	if err := json.Unmarshal([]byte(text.Text), &attrs); err != nil {
		t.Fatalf("failed to unmarshal result: %v\nraw: %s", err, text.Text)
	}
	return attrs
}

// TestLogFieldFilterField locks the raw-name -> filter_field convention. In
// particular, a real attribute whose name starts with "log_" must keep its full
// name (log_level -> attributes['log_level']), not be stripped to attributes['level'].
func TestLogFieldFilterField(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"service", "ServiceName"},
		{"severity", "SeverityText"},
		{"body", "Body"},
		{"status_code", "attributes['status_code']"},
		{"http.status_code", "attributes['http.status_code']"},
		{"log_level", "attributes['log_level']"}, // must NOT become attributes['level']
		{"log_id", "attributes['log_id']"},       // must NOT become attributes['id']
		{"resource_container_name", "resources['container_name']"},
		{"resource_k8s.namespace.name", "resources['k8s.namespace.name']"},
	}
	for _, c := range cases {
		if got := logFieldFilterField(c.name); got != c.want {
			t.Errorf("logFieldFilterField(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestGetLogAttributesForPipeline_UsesSeriesEndpoint verifies the tool POSTs the
// pipeline to /logs/api/v2/series/json and returns the union of field names from
// all label-sets, each mapped to the correct filter_field.
func TestGetLogAttributesForPipeline_UsesSeriesEndpoint(t *testing.T) {
	var capturedPath, capturedMethod string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if !strings.Contains(r.URL.Path, "/logs/api/v2/series/json") {
			// Body-sampling request — irrelevant to this test, return no rows.
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			return
		}
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)

		// Two label-sets with overlapping + distinct keys to exercise the union.
		_, _ = w.Write([]byte(`{"status":"success","data":[` +
			`{"service":"checkout-service","status_code":"500","resource_k8s.namespace.name":"prod"},` +
			`{"service":"checkout-service","uri":"/v1/orders"}` +
			`]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout-service"}}},
	}
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline:        pipeline,
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// Must POST to the series endpoint.
	if capturedMethod != "POST" || !strings.Contains(capturedPath, "/logs/api/v2/series/json") {
		t.Errorf("expected POST /logs/api/v2/series/json, got: %s %s", capturedMethod, capturedPath)
	}

	// Must forward the pipeline in the request body.
	if capturedBody == nil || capturedBody["pipeline"] == nil {
		t.Fatalf("expected pipeline forwarded in request body, got: %v", capturedBody)
	}

	// Union of keys, sorted, each with the correct filter_field.
	got := map[string]string{}
	for _, a := range decodeLogAttributes(t, res) {
		got[a.Name] = a.FilterField
	}

	want := map[string]string{
		"service":                     "ServiceName",
		"status_code":                 "attributes['status_code']",
		"resource_k8s.namespace.name": "resources['k8s.namespace.name']",
		"uri":                         "attributes['uri']",
	}
	if len(got) != len(want) {
		t.Errorf("expected %d unioned fields, got %d: %v", len(want), len(got), got)
	}
	for name, ff := range want {
		if got[name] != ff {
			t.Errorf("field %q: expected filter_field %q, got %q", name, ff, got[name])
		}
	}
}

// TestGetLogAttributesForPipeline_RequiresPipeline verifies the tool errors when
// no pipeline is provided.
func TestGetLogAttributesForPipeline_RequiresPipeline(t *testing.T) {
	cfg := testAttrConfig("http://unused.example")
	handler := NewGetLogAttributesForPipelineHandler(http.DefaultClient, cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{})
	if err == nil {
		t.Fatal("expected error when pipeline is empty, got nil")
	}
	if !strings.Contains(err.Error(), "pipeline parameter is required") {
		t.Errorf("expected pipeline-required error, got: %v", err)
	}
}

// bodySamplingServer builds an httptest server that serves a fixed series
// response and a fixed query_range (body-sampling) response, capturing the
// sampling request's pipeline, limit, and time window.
func bodySamplingServer(t *testing.T, seriesJSON string, sampleLines []string, capture *sampleCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/logs/api/v2/series/json") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(seriesJSON))
			return
		}
		if capture != nil {
			capture.requested = true
			capture.limit = r.URL.Query().Get("limit")
			capture.start = r.URL.Query().Get("start")
			capture.end = r.URL.Query().Get("end")
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &capture.body)
		}
		w.WriteHeader(http.StatusOK)
		values := make([]string, 0, len(sampleLines))
		for i, line := range sampleLines {
			enc, _ := json.Marshal(line)
			values = append(values, `["`+fmt.Sprintf("%d", 1700000000000000000+i)+`",`+string(enc)+`]`)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[` +
			strings.Join(values, ",") + `]}]}}`))
	}))
}

type sampleCapture struct {
	requested         bool
	limit, start, end string
	body              map[string]any
}

func attrByName(attrs []LogAttribute, name string) *LogAttribute {
	for i := range attrs {
		if attrs[i].Name == name {
			return &attrs[i]
		}
	}
	return nil
}

// TestGetLogAttributesForPipeline_BodyDerivedFields verifies that top-level keys
// of a JSON Body are reported as derived fields with source "body", a
// sample_coverage ratio, and a hint embedding the required parse stage — while
// keys already present as indexed attributes are reported once (indexed wins).
func TestGetLogAttributesForPipeline_BodyDerivedFields(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw","status_code":"500"}]}`
	samples := []string{
		`{"uri":"/v1/orders","status_code":500,"http_method":"GET"}`,
		`{"uri":"/v2/carts"}`,
	}
	var capture sampleCapture
	server := bodySamplingServer(t, series, samples, &capture)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	pipeline := []map[string]interface{}{
		{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "gw"}}},
	}
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline:        pipeline,
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)

	// Sampling request shape: caller's pipeline forwarded, bounded limit.
	if !capture.requested {
		t.Fatal("expected a body-sampling query_range request")
	}
	if capture.limit != "5" {
		t.Errorf("expected sampling limit 5, got %q", capture.limit)
	}
	if capture.body == nil || capture.body["pipeline"] == nil {
		t.Errorf("expected caller's pipeline forwarded in sampling request, got: %v", capture.body)
	}

	// Indexed key that also appears in Body: reported once, as indexed.
	var statusCount int
	for _, a := range attrs {
		if a.Name == "status_code" {
			statusCount++
			if a.Source == "body" {
				t.Errorf("status_code must stay indexed (indexed wins), got source %q", a.Source)
			}
		}
	}
	if statusCount != 1 {
		t.Errorf("expected exactly 1 status_code entry, got %d", statusCount)
	}

	// Body-derived keys with coverage.
	uri := attrByName(attrs, "uri")
	if uri == nil {
		t.Fatalf("expected body-derived field 'uri', got: %v", attrs)
	}
	if uri.Source != "body" {
		t.Errorf("uri: expected source \"body\", got %q", uri.Source)
	}
	if uri.SampleCoverage != "2/2" {
		t.Errorf("uri: expected sample_coverage \"2/2\", got %q", uri.SampleCoverage)
	}
	if uri.FilterField != "attributes['uri']" {
		t.Errorf("uri: expected filter_field attributes['uri'], got %q", uri.FilterField)
	}
	for _, frag := range []string{`"type":"parse"`, `"field":"Body"`, `attributes['uri']`} {
		if !strings.Contains(uri.Hint, frag) {
			t.Errorf("uri hint missing %q; hint: %s", frag, uri.Hint)
		}
	}

	method := attrByName(attrs, "http_method")
	if method == nil || method.Source != "body" || method.SampleCoverage != "1/2" {
		t.Errorf("expected http_method body-derived with coverage 1/2, got: %+v", method)
	}
}

// TestGetLogAttributesForPipeline_GloballySorted locks the documented contract:
// the combined indexed + body-derived array is sorted by name.
func TestGetLogAttributesForPipeline_GloballySorted(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw","status_code":"500"}]}`
	samples := []string{`{"uri":"/v1/a","latency":12}`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	for i := 1; i < len(attrs); i++ {
		if attrs[i-1].Name > attrs[i].Name {
			t.Fatalf("result not sorted by name: %q before %q", attrs[i-1].Name, attrs[i].Name)
		}
	}
}

// TestGetLogAttributesForPipeline_SamplingUsesRegionOverride verifies the body
// sampling request honors the args.Region override, like the series request.
func TestGetLogAttributesForPipeline_SamplingUsesRegionOverride(t *testing.T) {
	var sampleRegion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(r.URL.Path, "/logs/api/v2/series/json") {
			_, _ = w.Write([]byte(`{"status":"success","data":[{"service":"gw"}]}`))
			return
		}
		sampleRegion = r.URL.Query().Get("region")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
		Region:   "eu-west-1",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if sampleRegion != "eu-west-1" {
		t.Errorf("sampling request region = %q, want eu-west-1 (args.Region override)", sampleRegion)
	}
}

// TestGetLogAttributesForPipeline_UnsafeBodyKeysSkipped verifies keys that
// cannot be safely embedded in a JSON hint or attributes['<key>'] accessor are
// dropped rather than emitted broken.
func TestGetLogAttributesForPipeline_UnsafeBodyKeysSkipped(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw"}]}`
	samples := []string{`{"good_key":1,"bad\"quote":2,"bad'apostrophe":3,"bad key":4}`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	if attrByName(attrs, "good_key") == nil {
		t.Error("expected safe key 'good_key' to be reported")
	}
	for _, bad := range []string{`bad"quote`, `bad'apostrophe`, "bad key"} {
		if attrByName(attrs, bad) != nil {
			t.Errorf("unsafe key %q must be skipped, not emitted into hints/accessors", bad)
		}
	}
	// The surviving hint must be valid JSON.
	good := attrByName(attrs, "good_key")
	var hintPipeline []map[string]interface{}
	if err := json.Unmarshal([]byte(good.Hint), &hintPipeline); err != nil {
		t.Errorf("body-derived hint is not valid JSON: %v\nhint: %s", err, good.Hint)
	}
}

// TestGetLogAttributesForPipeline_SeverityFamilyDedup verifies that a
// body-derived severity-family field (level/severity) is suppressed when an
// indexed severity-family field is already present — semantic dedup, not just
// exact filter_field match. Indexed severity is the directly-filterable signal;
// a parallel body-derived level that needs a regexp/json parse is inferior.
func TestGetLogAttributesForPipeline_SeverityFamilyDedup(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw","severity":"INFO","status_code":"500"}]}`
	samples := []string{
		`trace_id= span_id= [main] 2026-07-08 08:34:00.267 INFO  com.example.Foo - starting up`,
		`{"severity":"warn","status_code":500,"uri":"/v1/a"}`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)

	// Indexed severity -> SeverityText must be present.
	var sawIndexedSeverity bool
	for _, a := range attrs {
		if a.Name == "severity" && a.FilterField == "SeverityText" && a.Source != "body" {
			sawIndexedSeverity = true
		}
		if a.Source == "body" && isSeverityFamilyName(a.Name) {
			t.Errorf("body-derived severity-family field %q must be suppressed when indexed severity exists; got %+v", a.Name, a)
		}
	}
	if !sawIndexedSeverity {
		t.Fatal("expected indexed severity (SeverityText) to remain")
	}

	// Non-severity body key from the JSON sample must still surface (JSON path
	// wins the cascade for this mixed sample).
	if uri := attrByName(attrs, "uri"); uri == nil || uri.Source != "body" {
		t.Errorf("expected non-severity body key 'uri' to remain, got: %v", attrs)
	}

	// status_code: indexed filter_field IS attributes['status_code'] — true
	// duplicate, body copy stays suppressed.
	var statusCount int
	for _, a := range attrs {
		if a.Name == "status_code" {
			statusCount++
		}
	}
	if statusCount != 1 {
		t.Errorf("expected exactly 1 status_code entry (true duplicate), got %d", statusCount)
	}
}

// TestGetLogAttributesForPipeline_BodyNotJSON verifies plain-text bodies yield
// no derived fields and no error.
func TestGetLogAttributesForPipeline_BodyNotJSON(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw","status_code":"500"}]}`
	samples := []string{`100.65.18.112 GET /v1/orders 500`, `plain text line`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	for _, a := range decodeLogAttributes(t, res) {
		if a.Source == "body" {
			t.Errorf("expected no body-derived fields for non-JSON bodies, got: %+v", a)
		}
	}
}

// TestGetLogAttributesForPipeline_LogfmtBody verifies logfmt-encoded Bodies
// (key=value pairs, not JSON) surface their keys as body-derived fields with
// a logfmt parse hint.
func TestGetLogAttributesForPipeline_LogfmtBody(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{
		`level=info msg="hi" status=200`,
		`level=error msg="boom" status=500`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)

	level := attrByName(attrs, "level")
	if level == nil || level.Source != "body" {
		t.Fatalf("expected body-derived 'level', got: %v", attrs)
	}
	if level.SampleCoverage != "2/2" {
		t.Errorf("level: expected sample_coverage 2/2, got %q", level.SampleCoverage)
	}
	if !strings.Contains(level.Hint, `"parser":"logfmt"`) {
		t.Errorf("level hint missing logfmt parser: %s", level.Hint)
	}

	status := attrByName(attrs, "status")
	if status == nil || status.Source != "body" {
		t.Fatalf("expected body-derived 'status', got: %v", attrs)
	}
	if !strings.Contains(status.Hint, `"parser":"logfmt"`) || !strings.Contains(status.Hint, "attributes['status']") {
		t.Errorf("status hint missing logfmt parser/filter field: %s", status.Hint)
	}

	// "msg" contains a quoted value with a space; it must still be captured
	// since the key itself is safe.
	if attrByName(attrs, "msg") == nil {
		t.Errorf("expected body-derived 'msg', got: %v", attrs)
	}
}

// TestGetLogAttributesForPipeline_PlaintextInlineLevel verifies a plaintext
// Body with an inline, timestamp-anchored severity token (neither JSON nor
// logfmt) surfaces "level" via a regexp parse hint whose pattern actually
// matches the sampled lines.
func TestGetLogAttributesForPipeline_PlaintextInlineLevel(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{
		`trace_id= span_id= [main] 2026-07-08 08:34:00.267 INFO  com.example.Foo - starting up`,
		`trace_id= span_id= [main] 2026-07-08 08:34:01.900 ERROR com.example.Foo - failed to connect`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)

	level := attrByName(attrs, "level")
	if level == nil {
		t.Fatalf("expected body-derived 'level', got: %v", attrs)
	}
	if level.Source != "body" {
		t.Errorf("level: expected source \"body\", got %q", level.Source)
	}
	if level.SampleCoverage != "2/2" {
		t.Errorf("level: expected sample_coverage 2/2, got %q", level.SampleCoverage)
	}
	if level.FilterField != "attributes['level']" {
		t.Errorf("level: expected filter_field attributes['level'], got %q", level.FilterField)
	}

	// Extract the pattern from the hint and confirm it actually matches an
	// ERROR line from the sample, and that the hint is a ready-to-use
	// two-stage parse+filter pipeline.
	var stages []map[string]interface{}
	if err := json.Unmarshal([]byte(level.Hint), &stages); err != nil {
		t.Fatalf("level hint is not valid JSON: %v\nhint: %s", err, level.Hint)
	}
	if len(stages) != 2 || stages[0]["type"] != "parse" || stages[0]["parser"] != "regexp" || stages[1]["type"] != "filter" {
		t.Fatalf("expected a 2-stage parse+filter pipeline, got: %s", level.Hint)
	}
	pattern, _ := stages[0]["pattern"].(string)
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("hint pattern does not compile: %v (%s)", err, pattern)
	}
	m := re.FindStringSubmatch(samples[1])
	if m == nil {
		t.Fatalf("hint pattern %q did not match sample ERROR line: %s", pattern, samples[1])
	}
	idx := re.SubexpIndex("level")
	if idx < 0 || !strings.EqualFold(m[idx], "ERROR") {
		t.Errorf("hint pattern did not capture level=ERROR, got match: %v", m)
	}
	// Filter stage must use the <value> placeholder (not a hardcoded severity
	// that may be absent from the sample). The pattern string itself may still
	// contain ERROR as a regex alternation — that is fine.
	filterQuery, _ := stages[1]["query"].(map[string]interface{})
	eq, _ := filterQuery["$eq"].([]interface{})
	if len(eq) < 2 || eq[1] != "<value>" {
		t.Errorf("level filter hint must use <value> placeholder, got: %v", eq)
	}
}

// TestGetLogAttributesForPipeline_PlaintextLowercaseLevel verifies the
// timestamp-anchored pattern is case-insensitive (common in Go/Python/Node).
func TestGetLogAttributesForPipeline_PlaintextLowercaseLevel(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{`2026-07-08 12:00:00.123 error something failed`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	level := attrByName(attrs, "level")
	if level == nil || level.Source != "body" {
		t.Fatalf("expected body-derived 'level' for lowercase severity, got: %v", attrs)
	}
}

// TestGetLogAttributesForPipeline_PlaintextBracketOrStartLevel verifies the
// conservative non-timestamp fallback: severity at line-start or in [].
func TestGetLogAttributesForPipeline_PlaintextBracketOrStartLevel(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{`[ERROR] failed to connect`, `INFO starting worker`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	level := attrByName(decodeLogAttributes(t, res), "level")
	if level == nil || level.Source != "body" {
		t.Fatalf("expected body-derived 'level' for bracket/start severity, got: %v", decodeLogAttributes(t, res))
	}
	if !strings.Contains(level.Hint, `"parser":"regexp"`) {
		t.Errorf("level hint missing regexp parser: %s", level.Hint)
	}
}

// TestGetLogAttributesForPipeline_UnstructuredNoLevel verifies a plaintext
// Body with no logfmt pairs and no recognizable severity token yields no
// fabricated body-derived fields — including URL path segments and prose that
// contain severity words mid-line (the old bare-token false-positive surface).
func TestGetLogAttributesForPipeline_UnstructuredNoLevel(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{
		`100.65.18.112 GET /v1/INFO 200`,
		`no ERROR occurred during the run`,
		`just some free text here`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	for _, a := range decodeLogAttributes(t, res) {
		if a.Source == "body" {
			t.Errorf("expected no fabricated body-derived fields, got: %+v", a)
		}
	}
}

// TestGetLogAttributesForPipeline_LogfmtProseRejected verifies incidental
// key=value fragments in prose are not classified as logfmt.
func TestGetLogAttributesForPipeline_LogfmtProseRejected(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{`set a=1 where b=2 in prose`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	for _, a := range decodeLogAttributes(t, res) {
		if a.Source == "body" {
			t.Errorf("prose with incidental key=value must not yield body-derived fields, got: %+v", a)
		}
	}
}

// TestGetLogAttributesForPipeline_SamplingFailureGraceful verifies a failing
// sampling request degrades to the indexed-only response without error.
func TestGetLogAttributesForPipeline_SamplingFailureGraceful(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/logs/api/v2/series/json") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[{"service":"gw","status_code":"500"}]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	if len(attrs) != 2 {
		t.Fatalf("expected the 2 indexed attributes, got: %v", attrs)
	}
}

// TestGetLogAttributesForPipeline_BodyKeyCap verifies body-derived keys are
// capped at 20, ranked by sample frequency then name.
func TestGetLogAttributesForPipeline_BodyKeyCap(t *testing.T) {
	series := `{"status":"success","data":[{"service":"gw"}]}`
	var sb strings.Builder
	sb.WriteString(`{"frequent":1`)
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&sb, `,"key_%02d":%d`, i, i)
	}
	sb.WriteString(`}`)
	samples := []string{sb.String(), `{"frequent":2}`}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var derived []LogAttribute
	for _, a := range decodeLogAttributes(t, res) {
		if a.Source == "body" {
			derived = append(derived, a)
		}
	}
	if len(derived) != 20 {
		t.Fatalf("expected body-derived keys capped at 20, got %d", len(derived))
	}
	// Highest-frequency key (present in both samples) must survive the cap.
	if attrByName(derived, "frequent") == nil {
		t.Error("expected highest-frequency key 'frequent' to survive the cap")
	}
}

// TestGetLogAttributesForPipeline_EmptyData verifies an empty series response
// yields an empty attribute list (not an error).
func TestGetLogAttributesForPipeline_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)

	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if attrs := decodeLogAttributes(t, res); len(attrs) != 0 {
		t.Errorf("expected empty attribute list, got: %v", attrs)
	}
}
