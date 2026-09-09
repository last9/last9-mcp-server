package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/otelids"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Synthetic identifiers: correct shape, obviously not real traffic.
const (
	testSpanIDAsTraceID = "abcdef0123456789"
	testValidTraceID    = "abcdef0123456789abcdef0123456789"
	testValidSpanID     = "0123456789abcdef"
)

// recordedRequest is what actually reached the wire. Counting alone cannot prove
// normalization: a handler forwarding a raw uppercase ID still makes one request.
type recordedRequest struct {
	URL  string
	Body string
}

type traceRequestRecorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (rec *traceRequestRecorder) add(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.requests = append(rec.requests, recordedRequest{URL: r.URL.String(), Body: string(body)})
}

func (rec *traceRequestRecorder) all() []recordedRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]recordedRequest(nil), rec.requests...)
}

func (rec *traceRequestRecorder) only(t *testing.T) recordedRequest {
	t.Helper()
	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 upstream request, got %d: %v", len(got), got)
	}
	return got[0]
}

func countingTraceDetailsServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	server, n, _ := recordingTraceDetailsServer(t)
	return server, n
}

func recordingTraceDetailsServer(t *testing.T) (*httptest.Server, *atomic.Int32, *traceRequestRecorder) {
	t.Helper()
	var n atomic.Int32
	rec := &traceRequestRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		rec.add(r)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/cat/api/traces/") {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"traces":[]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":{"result":[]}}`)
	}))
	t.Cleanup(server.Close)
	return server, &n, rec
}

func verifyTraceCfg(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "ap-south-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetTraceWaterfall_InvalidIDsMakeZeroUpstreamRequests(t *testing.T) {
	cases := []struct {
		name     string
		args     GetTraceWaterfallArgs
		category string
	}{
		{name: "empty", args: GetTraceWaterfallArgs{}, category: otelids.CategoryInvalidTraceID},
		{name: "short", args: GetTraceWaterfallArgs{TraceID: "abc123"}, category: otelids.CategoryInvalidTraceID},
		{name: "long", args: GetTraceWaterfallArgs{TraceID: testValidTraceID + "00"}, category: otelids.CategoryInvalidTraceID},
		{name: "non-hex", args: GetTraceWaterfallArgs{TraceID: "ea8148dece205073096e4ad48145b0zz"}, category: otelids.CategoryInvalidTraceID},
		{name: "all-zero", args: GetTraceWaterfallArgs{TraceID: strings.Repeat("0", 32)}, category: otelids.CategoryAllZeroID},
		{name: "16-hex span id", args: GetTraceWaterfallArgs{TraceID: testSpanIDAsTraceID}, category: otelids.CategorySpanIDAsTraceID},
		{name: "invalid selected span", args: GetTraceWaterfallArgs{TraceID: testValidTraceID, SelectedSpanID: "not-a-span"}, category: otelids.CategoryInvalidSpanID},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, n := countingTraceDetailsServer(t)
			handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tt.args)
			if err == nil {
				t.Fatal("expected local validation error")
			}
			if !strings.Contains(err.Error(), "category="+tt.category) {
				t.Fatalf("want category %s, got %v", tt.category, err)
			}
			if tt.category == otelids.CategorySpanIDAsTraceID && !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
				t.Fatalf("missing span-id message: %v", err)
			}
			if got := n.Load(); got != 0 {
				t.Fatalf("invalid ID made %d upstream requests, want 0", got)
			}
		})
	}
}

func TestGetTraceWaterfall_UppercaseIDsReachTheWireLowercased(t *testing.T) {
	server, n, rec := recordingTraceDetailsServer(t)
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID:        strings.ToUpper(testValidTraceID),
		SelectedSpanID: strings.ToUpper(testValidSpanID),
	})
	if err != nil {
		t.Fatalf("valid IDs should proceed: %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("valid waterfall made %d upstream requests, want 1", got)
	}

	sent := rec.only(t)
	if !strings.Contains(sent.URL, testValidTraceID) {
		t.Errorf("want lowercased trace ID in the upstream URL, got %q", sent.URL)
	}
	if strings.Contains(sent.URL, strings.ToUpper(testValidTraceID)) {
		t.Errorf("raw uppercase trace ID reached the wire: %q", sent.URL)
	}
}

// The tracejson mutation has to survive wrapTopLevelFilterQuery rebuilding the
// condition map, so assert on the forwarded body rather than the validator alone.
func TestGetTraces_UppercaseTraceIDInFilterReachesTheWireLowercased(t *testing.T) {
	server, _, rec := recordingTraceDetailsServer(t)
	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$eq": []interface{}{"TraceId", strings.ToUpper(testValidTraceID)},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:15:00Z",
	})
	if err != nil {
		t.Fatalf("valid uppercase trace ID should proceed: %v", err)
	}

	sent := rec.all()
	if len(sent) == 0 {
		t.Fatal("want at least 1 upstream request, got 0")
	}
	for _, req := range sent {
		if strings.Contains(req.Body, strings.ToUpper(testValidTraceID)) {
			t.Errorf("raw uppercase trace ID reached the wire in body: %s", req.Body)
		}
		if !strings.Contains(req.Body, testValidTraceID) {
			t.Errorf("want lowercased trace ID in the forwarded body, got: %s", req.Body)
		}
	}
}

func TestGetServiceTraces_InvalidTraceIDMakesZeroUpstreamRequests(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := GetServiceTracesHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetServiceTracesArgs{
		TraceID: testSpanIDAsTraceID,
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
		t.Fatalf("got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("get_service_traces made %d upstream requests, want 0", got)
	}
}

func TestGetTraces_InvalidExactTraceIDMakesZeroUpstreamRequests(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTracesHandler(server.Client(), testChunkTracesConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTracesArgs{
		TracejsonQuery: []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": []interface{}{
						map[string]interface{}{
							"$eq": []interface{}{"TraceId", testSpanIDAsTraceID},
						},
					},
				},
			},
		},
		StartTimeISO: "1970-01-01T00:00:00Z",
		EndTimeISO:   "1970-01-01T00:15:00Z",
	})
	if err == nil {
		t.Fatal("expected local validation error")
	}
	if !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
		t.Fatalf("got %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("get_traces made %d upstream requests, want 0", got)
	}
}

func TestGetTraceWaterfallInputSchemaOmitsIDPatterns(t *testing.T) {
	schema := GetTraceWaterfallInputSchema()
	props := schema["properties"].(map[string]interface{})
	for _, field := range []string{"trace_id", "selected_span_id"} {
		if p, ok := props[field].(map[string]interface{})["pattern"]; ok {
			t.Fatalf("%s must not carry a schema pattern, got %v", field, p)
		}
	}
}

func TestSpanIDAsTraceIDKeepsActionableMessage(t *testing.T) {
	server, n := countingTraceDetailsServer(t)
	handler := NewGetTraceWaterfallHandler(server.Client(), verifyTraceCfg(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceWaterfallArgs{
		TraceID: testSpanIDAsTraceID,
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "span ID where a trace ID is required") {
		t.Fatalf("error must name the span-ID mistake, got: %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("expected zero upstream requests, got %d", got)
	}
}

func filterStage(query map[string]interface{}) []map[string]interface{} {
	return []map[string]interface{}{{"type": "filter", "query": query}}
}

func TestSanitizeTraceJSONQuery_ValidatesIDsForAllExactMatchOperators(t *testing.T) {
	for _, operator := range []string{"$eq", "$ieq", "$neq", "$ineq"} {
		t.Run(operator, func(t *testing.T) {
			stages := filterStage(map[string]interface{}{
				operator: []interface{}{"TraceId", testSpanIDAsTraceID},
			})
			err := SanitizeTraceJSONQuery(stages)
			if err == nil {
				t.Fatalf("%s forwarded a 16-hex value as a trace ID comparison", operator)
			}
			if !strings.Contains(err.Error(), "category="+otelids.CategorySpanIDAsTraceID) {
				t.Fatalf("want span-id-as-trace-id category, got %v", err)
			}
		})
	}
}

func TestSanitizeTraceJSONQuery_NormalizesIDsForAllExactMatchOperators(t *testing.T) {
	for _, operator := range []string{"$eq", "$ieq", "$neq", "$ineq"} {
		t.Run(operator, func(t *testing.T) {
			stages := filterStage(map[string]interface{}{
				operator: []interface{}{"TraceId", strings.ToUpper(testValidTraceID)},
			})
			if err := SanitizeTraceJSONQuery(stages); err != nil {
				t.Fatalf("valid uppercase ID rejected: %v", err)
			}
			encoded, err := json.Marshal(stages)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), testValidTraceID) {
				t.Errorf("%s did not normalize in place: %s", operator, encoded)
			}
		})
	}
}

// The $notnull rewrite produces {"$neq": [field, ""]} before validation runs, and
// an empty operand is also how a model spells an existence check by hand. Neither
// is an ID, so both must survive now that $neq validates IDs.
func TestSanitizeTraceJSONQuery_ExistenceIdiomOnIDFieldsSurvives(t *testing.T) {
	for _, query := range []map[string]interface{}{
		{"$neq": []interface{}{"TraceId", ""}},
		{"$notnull": []interface{}{"TraceId"}},
		{"$neq": []interface{}{"SpanId", ""}},
	} {
		stages := filterStage(query)
		if err := SanitizeTraceJSONQuery(stages); err != nil {
			t.Errorf("existence check %v rejected: %v", query, err)
		}
	}
}

// additionalProperties:false plus pre-handler validation means a field added to
// the struct but not the hand-written schema is rejected before the handler runs.
func TestGetTraceWaterfallInputSchemaMatchesArgs(t *testing.T) {
	schema := GetTraceWaterfallInputSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema has no properties object")
	}

	argsType := reflect.TypeOf(GetTraceWaterfallArgs{})
	for i := 0; i < argsType.NumField(); i++ {
		name := strings.Split(argsType.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if _, present := properties[name]; !present {
			t.Errorf("GetTraceWaterfallArgs field %q is missing from GetTraceWaterfallInputSchema", name)
		}
		delete(properties, name)
	}
	for leftover := range properties {
		t.Errorf("GetTraceWaterfallInputSchema declares %q with no matching GetTraceWaterfallArgs field", leftover)
	}
}
