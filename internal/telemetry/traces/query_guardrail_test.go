package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/guardrail"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// guardrailServer answers the estimate route with estimate (status 200) or
// estimateStatus, and every other route with one span.
type guardrailServer struct {
	*httptest.Server
	estimateCalls atomic.Int32
	queryCalls    atomic.Int32
}

func newGuardrailServer(t *testing.T, estimateStatus int, estimate string) *guardrailServer {
	t.Helper()
	s := &guardrailServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/estimate") {
			s.estimateCalls.Add(1)
			w.WriteHeader(estimateStatus)
			_, _ = w.Write([]byte(estimate))
			return
		}
		s.queryCalls.Add(1)
		_, _ = w.Write([]byte(traceAPIResponse(1)))
	}))
	t.Cleanup(s.Close)
	return s
}

func guardrailConfig(url, mode string) models.Config {
	cfg := testChunkTracesConfig(url)
	cfg.QueryGuardrail = models.QueryGuardrail{
		Mode:                mode,
		RangeCap:            24 * time.Hour,
		ScopeThresholdBytes: 1000,
		QueryBudgetBytes:    5000,
	}
	return cfg
}

var serviceSpanFilter = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}}},
}

const twoDaysMinutes = 48 * 60

func callGuardedGetTraces(t *testing.T, s *guardrailServer, mode string, args GetTracesArgs) *mcp.CallToolResult {
	t.Helper()
	result, _, err := NewGetTracesHandler(s.Client(), guardrailConfig(s.URL, mode))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func envelopeHint(t *testing.T, result *mcp.CallToolResult, prefix string) string {
	t.Helper()
	for _, h := range utils.GetResultEnvelope(t, result).Hints {
		if strings.HasPrefix(h, prefix) {
			return h
		}
	}
	return ""
}

func TestGetTracesGuardrailEnforceRejectNeverQueries(t *testing.T) {
	tests := []struct {
		name     string
		estimate string
		args     GetTracesArgs
		reason   string
	}{
		{"unscoped window over the cap", `{"data":{"bytes":10}}`,
			GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: twoDaysMinutes}, guardrail.ReasonRangeTooWide},
		{"unscoped scan over the scope threshold", `{"data":{"bytes":1001}}`,
			GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: 60}, guardrail.ReasonUnscopedQuery},
		{"scoped scan over the budget", `{"data":{"bytes":5001}}`,
			GetTracesArgs{TracejsonQuery: serviceSpanFilter, LookbackMinutes: 60}, guardrail.ReasonScanBudgetExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newGuardrailServer(t, http.StatusOK, tt.estimate)
			result := callGuardedGetTraces(t, s, guardrail.ModeEnforce, tt.args)

			if n := s.queryCalls.Load(); n != 0 {
				t.Fatalf("upstream query called %d times on an enforce reject", n)
			}
			if !result.IsError || !strings.Contains(utils.GetTextContent(t, result), tt.reason) {
				t.Fatalf("isError=%v text=%q", result.IsError, utils.GetTextContent(t, result))
			}
			if envelopeHint(t, result, "query_guardrail["+tt.reason+"] rejected") == "" {
				t.Errorf("envelope lacks the %s hint", tt.reason)
			}
		})
	}
}

func TestGetTracesGuardrailShadowRunsAndHints(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetTraces(t, s, guardrail.ModeShadow, GetTracesArgs{TracejsonQuery: serviceSpanFilter, LookbackMinutes: 60})

	if result.IsError || s.queryCalls.Load() == 0 {
		t.Fatalf("shadow blocked the query: isError=%v queries=%d", result.IsError, s.queryCalls.Load())
	}
	if envelopeHint(t, result, "query_guardrail["+guardrail.ReasonScanBudgetExceeded+"] would be rejected") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
}

func TestGetTracesGuardrailOffSkipsEstimate(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetTraces(t, s, guardrail.ModeOff, GetTracesArgs{TracejsonQuery: nonEmptyServiceFilter, LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.estimateCalls.Load() != 0 || s.queryCalls.Load() == 0 {
		t.Fatalf("isError=%v estimates=%d queries=%d", result.IsError, s.estimateCalls.Load(), s.queryCalls.Load())
	}
}

func callGuardedGetServiceTraces(t *testing.T, s *guardrailServer, mode string, args GetServiceTracesArgs) *mcp.CallToolResult {
	t.Helper()
	result, _, err := GetServiceTracesHandler(s.Client(), guardrailConfig(s.URL, mode))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func TestGetServiceTracesGuardrailEnforceRejectNeverQueries(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetServiceTraces(t, s, guardrail.ModeEnforce, GetServiceTracesArgs{ServiceName: "api", LookbackMinutes: 60})

	if n := s.queryCalls.Load(); n != 0 {
		t.Fatalf("upstream query called %d times on an enforce reject", n)
	}
	if !result.IsError || envelopeHint(t, result, "query_guardrail["+guardrail.ReasonScanBudgetExceeded+"] rejected") == "" {
		t.Fatalf("isError=%v hints=%v", result.IsError, utils.GetResultEnvelope(t, result).Hints)
	}
}

func TestGetServiceTracesGuardrailShadowRunsAndHints(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetServiceTraces(t, s, guardrail.ModeShadow, GetServiceTracesArgs{ServiceName: "api", LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.queryCalls.Load() != 1 {
		t.Fatalf("shadow blocked the query: isError=%v queries=%d", result.IsError, s.queryCalls.Load())
	}
	if !strings.HasPrefix(utils.GetTextContent(t, result), `{"data":[{"trace_id":"trace-0"`) {
		t.Errorf("payload text changed: %s", utils.GetTextContent(t, result))
	}
	if envelopeHint(t, result, "query_guardrail["+guardrail.ReasonScanBudgetExceeded+"] would be rejected") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
	if envelopeHint(t, result, "query_guardrail["+guardrail.ReasonRangeTooWide+"]") != "" {
		t.Error("a service-scoped query was held to the unscoped range cap")
	}
}

// An exact trace lookup reads one trace by id from its own endpoint; there is
// no scan to estimate.
func TestGetServiceTracesGuardrailSkipsTraceIDLookup(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetServiceTraces(t, s, guardrail.ModeEnforce, GetServiceTracesArgs{TraceID: "ea8148dece205073096e4ad48145b08a"})

	if result.IsError || s.estimateCalls.Load() != 0 || s.queryCalls.Load() != 1 {
		t.Fatalf("isError=%v estimates=%d queries=%d", result.IsError, s.estimateCalls.Load(), s.queryCalls.Load())
	}
}

// The tracejson sanitizer accepts `where` as a `filter` alias, so a scoped
// `where` must escape the unscoped range cap when no estimate is available.
func TestGetTracesGuardrailScopedStageAliasesSkipRangeCap(t *testing.T) {
	for _, stage := range []string{"filter", "where"} {
		t.Run(stage, func(t *testing.T) {
			s := newGuardrailServer(t, http.StatusNotFound, `404 page not found`)
			pipeline := []map[string]interface{}{
				{"type": stage, "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "checkout"}}},
			}
			result := callGuardedGetTraces(t, s, guardrail.ModeEnforce, GetTracesArgs{TracejsonQuery: pipeline, LookbackMinutes: twoDaysMinutes})

			if result.IsError || s.queryCalls.Load() == 0 {
				t.Fatalf("service-scoped %s query blocked: isError=%v queries=%d text=%q",
					stage, result.IsError, s.queryCalls.Load(), utils.GetTextContent(t, result))
			}
		})
	}
}

func TestGetTracesGuardrailEstimateUnavailableStillRuns(t *testing.T) {
	s := newGuardrailServer(t, http.StatusBadRequest, `{"error":"bad pipeline"}`)
	result := callGuardedGetTraces(t, s, guardrail.ModeEnforce, GetTracesArgs{TracejsonQuery: serviceSpanFilter, LookbackMinutes: 60})

	if result.IsError || s.queryCalls.Load() == 0 {
		t.Fatalf("estimate failure blocked the query: isError=%v queries=%d", result.IsError, s.queryCalls.Load())
	}
	if envelopeHint(t, result, "query_guardrail["+guardrail.ReasonEstimateUnavailable+"]") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
}
