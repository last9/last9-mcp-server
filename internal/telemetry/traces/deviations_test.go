package traces

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuildDeviationAPIRequestUsesSameWindowForErrors(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	request, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production", LookbackMinutes: 10,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Comparison.Target != request.Comparison.Control {
		t.Fatal("errors mode must use one window for error and non-error cohorts")
	}
	if request.Comparison.Target.End.Sub(request.Comparison.Target.Start) != 10*time.Minute {
		t.Fatalf("unexpected target window: %+v", request.Comparison.Target)
	}
	if !request.Candidates.AutoDiscover || request.ContractVersion != attributeDeviationsVersion {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestBuildDeviationAPIRequestValidatesTimeAndLatencyModes(t *testing.T) {
	timeArgs := GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout", Environment: "production",
		StartTimeISO: "2026-07-15T11:45:00Z", EndTimeISO: "2026-07-15T12:00:00Z",
		BaselineStartISO: "2026-07-15T11:30:00Z", BaselineEndISO: "2026-07-15T11:45:00Z",
	}
	if _, err := buildDeviationAPIRequest(timeArgs, time.Now()); err != nil {
		t.Fatal(err)
	}
	timeArgs.BaselineEndISO = "2026-07-15T11:46:00Z"
	if _, err := buildDeviationAPIRequest(timeArgs, time.Now()); err == nil || !strings.Contains(err.Error(), "equal in duration") {
		t.Fatalf("expected equal-duration error, got %v", err)
	}
	latencyArgs := GetTraceAttributeDeviationsArgs{ComparisonMode: "latency", ServiceName: "checkout", Environment: "production"}
	if _, err := buildDeviationAPIRequest(latencyArgs, time.Now()); err == nil || !strings.Contains(err.Error(), "latency_threshold_ms") {
		t.Fatalf("expected latency threshold error, got %v", err)
	}
}

func TestTraceAttributeDeviationsHandlerCallsAtomicEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != attributeDeviationsEndpoint || r.URL.Query().Get("region") != "test" {
			t.Fatalf("unexpected request URL: %s", r.URL.String())
		}
		if r.Header.Get("X-LAST9-API-TOKEN") != "Bearer test-token" {
			t.Fatalf("missing API token header")
		}
		var request deviationAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Scope.ServiceName != "last9-api" || request.Comparison.Mode != "errors" || !request.Candidates.AutoDiscover {
			t.Fatalf("unexpected payload: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, `"analysis_version":"trace-attribute-deviations/v1"`) {
		t.Fatalf("unexpected MCP result: %+v", result.Content)
	}
}

// The sanitizer must not forward a {"$and":[...]} logical operator as a single
// filters-array element. When a filters element mixes $notnull/$exists with a
// sibling field operator, the shared existence rewriter folds it to $and; the
// deviations path must split that $and back into sibling bare conditions before
// the request hits the wire. Asserting on the decoded upstream body (not the
// sanitizer's in-memory output) proves the canonical shape actually reaches
// the endpoint.
func TestTraceAttributeDeviationsHandler_SplitsFoldedFiltersOnTheWire(t *testing.T) {
	var captured deviationAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{
				"$notnull": []interface{}{"SpanName"},
				"$eq":      []interface{}{"SpanKind", "SPAN_KIND_SERVER"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured.Scope.Filters) != 2 {
		t.Fatalf("want 2 bare filters elements on the wire, got %d: %v",
			len(captured.Scope.Filters), captured.Scope.Filters)
	}
	for i, cond := range captured.Scope.Filters {
		for key := range cond {
			if _, isLogical := traceFilterLogicalOperators[key]; isLogical {
				t.Errorf("filters element %d forwarded a %q logical operator (non-canonical): %v", i, key, cond)
			}
		}
	}
	raw, _ := json.Marshal(captured.Scope.Filters)
	body := string(raw)
	for _, want := range []string{
		`{"$eq":["SpanKind","SPAN_KIND_SERVER"]}`,
		`{"$neq":["SpanName",""]}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing expected bare condition %s on the wire, got %s", want, body)
		}
	}
	if strings.Contains(body, "$notnull") || strings.Contains(body, "$exists") {
		t.Fatalf("broken existence operator reached the wire: %s", body)
	}
}

// A filters array with each operator already in its own element (the idiomatic
// shape) must be forwarded unchanged: the split fix must not regrow or reorder
// the happy path.
func TestTraceAttributeDeviationsHandler_ForwardsBareFiltersUnchanged(t *testing.T) {
	var captured deviationAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{"$neq": []interface{}{"SpanName", ""}},
			{"$eq": []interface{}{"SpanKind", "SPAN_KIND_SERVER"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured.Scope.Filters) != 2 {
		t.Fatalf("want 2 filters elements, got %d", len(captured.Scope.Filters))
	}
	if len(captured.Scope.Filters[0]) != 1 || captured.Scope.Filters[0]["$neq"] == nil {
		t.Errorf("first element changed shape: %v", captured.Scope.Filters[0])
	}
	if len(captured.Scope.Filters[1]) != 1 || captured.Scope.Filters[1]["$eq"] == nil {
		t.Errorf("second element changed shape: %v", captured.Scope.Filters[1])
	}
}

// 5xx bodies can carry backend query text; only 400/422 are relayed, sanitized.
func TestTraceAttributeDeviationsHandlerDoesNotEchoServerErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		w.Write([]byte(`{"code":"query_timeout","detail":"attribute deviation query timed out"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	})
	if err == nil {
		t.Fatal("expected an error for a 504 response")
	}
	for _, leaked := range []string{"query_timeout", "attribute deviation query timed out"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("upstream 5xx body must not be echoed, got %q", err.Error())
		}
	}
	if !strings.Contains(err.Error(), "504") || !isTraceUpstreamError(err) {
		t.Fatalf("expected a sanitized trace upstream error, got %q", err.Error())
	}
}

// A 400 is a caller mistake, so the sanitized upstream body is still relayed.
func TestTraceAttributeDeviationsHandlerRelaysSanitizedBadRequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"invalid_filter","detail":"unknown field xyz"}`))
	}))
	defer server.Close()
	handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_filter") {
		t.Fatalf("expected the 400 body to be relayed, got %v", err)
	}
}

// Documented bounds must be applied, and auto-discover must not ask for 0 candidates.
func TestDeviationLimitsApplyDocumentedDefaultsAndBounds(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	request, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	// Only limits this tool chooses are sent; the endpoint owns the rest.
	want := deviationAPILimits{
		MinimumCohortSize:    100,
		MinimumValueSupport:  20,
		MaximumRankedResults: 10,
	}
	if request.Limits != want {
		t.Fatalf("limits=%+v, want %+v", request.Limits, want)
	}

	explicit, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production",
		CandidateAttributes: []string{"a", "b"}, MinimumCohortSize: 50, MinimumValueSupport: 15, Limit: 5,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Limits.MinimumCohortSize != 50 ||
		explicit.Limits.MinimumValueSupport != 15 || explicit.Limits.MaximumRankedResults != 5 {
		t.Fatalf("limits=%+v", explicit.Limits)
	}
}

func TestDeviationLimitsRejectOutOfRangeArguments(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	base := GetTraceAttributeDeviationsArgs{ComparisonMode: "errors", ServiceName: "last9-api", Environment: "production"}
	cases := []struct {
		name    string
		mutate  func(*GetTraceAttributeDeviationsArgs)
		wantErr string
	}{
		{"too many candidates", func(a *GetTraceAttributeDeviationsArgs) {
			a.CandidateAttributes = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
		}, "candidate_attributes"},
		{"limit above maximum", func(a *GetTraceAttributeDeviationsArgs) { a.Limit = 999 }, "limit must be between"},
		{"cohort below minimum", func(a *GetTraceAttributeDeviationsArgs) { a.MinimumCohortSize = 5 }, "minimum_cohort_size"},
		{"value support below minimum", func(a *GetTraceAttributeDeviationsArgs) { a.MinimumValueSupport = 1 }, "minimum_value_support"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := base
			c.mutate(&args)
			if _, err := buildDeviationAPIRequest(args, now); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected %q error, got %v", c.wantErr, err)
			}
		})
	}
}

func TestDeviationBaselineAcceptsSameFormatsAsTarget(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	base := GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout", Environment: "production",
		StartTimeISO: "2026-07-15 11:45:00", EndTimeISO: "2026-07-15 12:00:00",
	}
	for _, layout := range []struct{ name, start, end string }{
		{"space separated", "2026-07-15 11:00:00", "2026-07-15 11:15:00"},
		{"rfc3339", "2026-07-15T11:00:00Z", "2026-07-15T11:15:00Z"},
		{"rfc3339 nano", "2026-07-15T11:00:00.000000000Z", "2026-07-15T11:15:00.000000000Z"},
	} {
		t.Run(layout.name, func(t *testing.T) {
			args := base
			args.BaselineStartISO, args.BaselineEndISO = layout.start, layout.end
			request, err := buildDeviationAPIRequest(args, now)
			if err != nil {
				t.Fatalf("baseline %q rejected while the same format is valid in start_time_iso: %v", layout.start, err)
			}
			if !request.Comparison.Control.Start.Equal(time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)) {
				t.Fatalf("control start=%v", request.Comparison.Control.Start)
			}
		})
	}
}

// Without the value, a caller cannot tell which format was wanted.
func TestDeviationBaselineErrorNamesTheReceivedValue(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	_, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "time", ServiceName: "checkout", Environment: "production",
		BaselineStartISO: "15/07/2026", BaselineEndISO: "2026-07-15T11:15:00Z",
	}, now)
	if err == nil {
		t.Fatal("expected an unparseable baseline to fail")
	}
	for _, want := range []string{"baseline_start_time_iso", `"15/07/2026"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must contain %s", err, want)
		}
	}
}

// The waterfall path rejects a negative lookback; this one used to drop it and fall
// through to the default window.
func TestDeviationLookbackMinutesRejectsOutOfRangeValues(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	base := GetTraceAttributeDeviationsArgs{ComparisonMode: "errors", ServiceName: "checkout", Environment: "production"}
	for _, lookback := range []int{-1, -60, 60} {
		args := base
		args.LookbackMinutes = lookback
		_, err := buildDeviationAPIRequest(args, now)
		if err == nil {
			t.Fatalf("lookback_minutes=%d must be rejected", lookback)
		}
		if !strings.Contains(err.Error(), "lookback_minutes") {
			t.Fatalf("lookback_minutes=%d error %q must name the argument", lookback, err)
		}
	}
}

// tracesTestConfig is the package-wide mock config builder for
// httptest-backed handler tests.
func tracesTestConfig(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		Region:     "test",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

// Endpoint-owned budgets must be omitted, not pinned to its current tuning.
func TestDeviationDiscoveryOmitsEndpointOwnedLimits(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	request, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "checkout", Environment: "production",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"maximum_candidates", "maximum_values_per_attribute", "representatives_per_result"} {
		if strings.Contains(string(body), absent) {
			t.Fatalf("%s must be omitted on the discovery path: %s", absent, body)
		}
	}

	// Omitted on the explicit path too: the endpoint can count the list it was sent.
	explicit, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode: "errors", ServiceName: "checkout", Environment: "production",
		CandidateAttributes: []string{"a", "b"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err = json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"maximum_candidates", "maximum_values_per_attribute", "representatives_per_result"} {
		if strings.Contains(string(body), absent) {
			t.Fatalf("%s must be omitted on the explicit path: %s", absent, body)
		}
	}
}

// With no filters, the request must omit the filters field entirely (omitempty)
// and must not introduce a $and. Confirms the new returned-slice path doesn't
// emit an empty array or a stray logical operator on the happy path.
func TestDeviationEmptyFiltersOmitsFiltersField(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for _, args := range []GetTraceAttributeDeviationsArgs{
		{ComparisonMode: "errors", ServiceName: "checkout", Environment: "production"},
		{ComparisonMode: "errors", ServiceName: "checkout", Environment: "production", Filters: nil},
		{ComparisonMode: "errors", ServiceName: "checkout", Environment: "production", Filters: []map[string]interface{}{}},
	} {
		request, err := buildDeviationAPIRequest(args, now)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"filters"`) {
			t.Fatalf("filters key must be omitted when there are no filters: %s", body)
		}
		if strings.Contains(string(body), "$and") {
			t.Fatalf("no $and may be synthesized on the empty path: %s", body)
		}
	}
}

// TraceId/SpanId/ParentSpanId/TraceState/Timestamp are valid get_traces fields
// but the deviations endpoint rejects them with HTTP 422. Fail closed locally
// (including when the banned field is inside a folded multi-op element) and
// make zero upstream requests.
func TestDeviationDisallowedFilterFieldsMakeZeroUpstreamRequests(t *testing.T) {
	for _, field := range []string{"TraceId", "SpanId", "ParentSpanId", "TraceState", "Timestamp"} {
		field := field
		t.Run(field, func(t *testing.T) {
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"contract_version":"investigation-evidence/v1","analysis_version":"trace-attribute-deviations/v1"}`)
			}))
			defer server.Close()
			handler := NewGetTraceAttributeDeviationsHandler(server.Client(), tracesTestConfig(server.URL))
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
				ComparisonMode:     "latency",
				ServiceName:        "checkout",
				Environment:        "prod",
				LatencyThresholdMs: 100,
				Filters: []map[string]interface{}{
					{
						"$notnull": []interface{}{"SpanName"},
						"$eq":      []interface{}{field, "x"},
					},
				},
			})
			if err == nil {
				t.Fatalf("expected local rejection of deviations filter field %q", field)
			}
			if !strings.Contains(err.Error(), "invalid filter field") || !strings.Contains(err.Error(), field) {
				t.Fatalf("want invalid filter field %q, got %v", field, err)
			}
			if got := n.Load(); got != 0 {
				t.Fatalf("disallowed field made %d upstream requests, want 0", got)
			}
		})
	}
}

// Allowed cohort fields (and attributes['…']) must still pass local validation
// so the denylist does not over-reject.
func TestDeviationAllowsCohortFilterFields(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for _, field := range []string{"ServiceName", "SpanName", "SpanKind", "StatusCode", "Duration", "StatusMessage"} {
		_, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
			ComparisonMode:     "latency",
			ServiceName:        "checkout",
			Environment:        "prod",
			LatencyThresholdMs: 100,
			Filters: []map[string]interface{}{
				{"$neq": []interface{}{field, ""}},
			},
		}, now)
		if err != nil {
			t.Fatalf("field %q should be allowed: %v", field, err)
		}
	}
	_, err := buildDeviationAPIRequest(GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{"$eq": []interface{}{"attributes['http.method']", "GET"}},
		},
	}, now)
	if err != nil {
		t.Fatalf("attributes filter should be allowed: %v", err)
	}
}

// Integration test against the live Last9 attribute-deviations endpoint. It is
// gated by TEST_REFRESH_TOKEN / LAST9_REFRESH_TOKEN via SetupTestConfigOrSkip;
// without a token it skips (the repo convention for all *_Integration tests).
// With a token set, the trigger filters — a single element mixing $notnull with
// a sibling $eq — exercise the fix end to end: the sanitizer must split the
// folded $and into sibling bare conditions and the upstream must accept the
// canonical flat filters shape (200, evidence-contract-conformant), proving no
// 400 regression from the split. SpanName/SpanKind are used because identity
// fields (TraceId, …) are rejected both locally and upstream.
func TestGetTraceAttributeDeviationsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetTraceAttributeDeviationsHandler(http.DefaultClient, *cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTraceAttributeDeviationsArgs{
		ComparisonMode:     "latency",
		ServiceName:        "checkout",
		Environment:        "prod",
		LatencyThresholdMs: 100,
		Filters: []map[string]interface{}{
			{
				"$notnull": []interface{}{"SpanName"},
				"$eq":      []interface{}{"SpanKind", "SPAN_KIND_SERVER"},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected upstream to accept split filters; got: %v", err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(text.Text), &envelope); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if envelope["contract_version"] != investigationEvidenceVersion {
		t.Fatalf("contract_version=%v, want %q", envelope["contract_version"], investigationEvidenceVersion)
	}
	if envelope["analysis_version"] != attributeDeviationsVersion {
		t.Fatalf("analysis_version=%v, want %q", envelope["analysis_version"], attributeDeviationsVersion)
	}
	t.Logf("deviations endpoint accepted the split filters shape; evidence: %s", text.Text)
}
