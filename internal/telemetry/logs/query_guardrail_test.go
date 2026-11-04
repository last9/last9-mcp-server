package logs

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/guardrail"
	"last9-mcp/internal/telemetry/guardrail/guardrailtest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newGuardrailServer answers every query with one matching log line.
func newGuardrailServer(t *testing.T, estimateStatus int, estimate string) *guardrailtest.Server {
	return guardrailtest.NewServer(t, estimateStatus, estimate,
		strings.Replace(streamsAPIResponse(nLogValues(1, "boom")), "{", `{"status":"success",`, 1))
}

func guardrailConfig(url, mode string) models.Config {
	cfg := testLogsConfig(url)
	cfg.QueryGuardrail = guardrailtest.Limits(mode)
	return cfg
}

var serviceErrorFilter = []map[string]interface{}{
	{"type": "filter", "query": map[string]interface{}{"$and": []interface{}{
		map[string]interface{}{"$eq": []interface{}{"ServiceName", "api"}},
		map[string]interface{}{"$contains": []interface{}{"Body", "error"}},
	}}},
}

const twoDaysMinutes = 48 * 60

func callGuardedGetLogs(t *testing.T, s *guardrailtest.Server, mode string, args GetLogsArgs) *mcp.CallToolResult {
	t.Helper()
	result, _, err := NewGetLogsHandler(s.Client(), guardrailConfig(s.URL, mode))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func TestGetLogsGuardrailEnforceRejectNeverQueries(t *testing.T) {
	tests := []struct {
		name     string
		estimate string
		args     GetLogsArgs
		reason   string
	}{
		{"unscoped window over the cap", `{"data":{"bytes":10}}`,
			GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: twoDaysMinutes}, guardrail.ReasonRangeTooWide},
		{"unscoped scan over the scope threshold", `{"data":{"bytes":1001}}`,
			GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 60}, guardrail.ReasonUnscopedQuery},
		{"scoped scan over the budget", `{"data":{"bytes":5001}}`,
			GetLogsArgs{LogjsonQuery: serviceErrorFilter, LookbackMinutes: 60}, guardrail.ReasonScanBudgetExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newGuardrailServer(t, http.StatusOK, tt.estimate)
			result := callGuardedGetLogs(t, s, guardrail.ModeEnforce, tt.args)

			if n := s.QueryCalls.Load(); n != 0 {
				t.Fatalf("upstream query called %d times on an enforce reject", n)
			}
			if !result.IsError {
				t.Fatal("an enforce reject must be a tool error")
			}
			if !strings.Contains(utils.GetTextContent(t, result), tt.reason) {
				t.Errorf("text %q lacks %s", utils.GetTextContent(t, result), tt.reason)
			}
			if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+tt.reason+"] rejected") == "" {
				t.Errorf("envelope lacks the %s hint", tt.reason)
			}
		})
	}
}

func TestGetLogsGuardrailShadowRunsAndHints(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":10}}`)
	result := callGuardedGetLogs(t, s, guardrail.ModeShadow, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.QueryCalls.Load() == 0 {
		t.Fatalf("shadow blocked the query: isError=%v queries=%d", result.IsError, s.QueryCalls.Load())
	}
	if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonRangeTooWide+"] would be rejected") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
}

func TestGetLogsGuardrailAllowedQueryRunsWithoutHints(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":10}}`)
	result := callGuardedGetLogs(t, s, guardrail.ModeEnforce, GetLogsArgs{LogjsonQuery: serviceErrorFilter, LookbackMinutes: 60})

	if result.IsError || s.QueryCalls.Load() == 0 || s.EstimateCalls.Load() != 1 {
		t.Fatalf("isError=%v queries=%d estimates=%d", result.IsError, s.QueryCalls.Load(), s.EstimateCalls.Load())
	}
	if h := guardrailtest.EnvelopeHint(t, result, "query_guardrail"); h != "" {
		t.Fatalf("allowed query carries a guardrail hint: %q", h)
	}
}

func TestGetLogsGuardrailOffSkipsEstimate(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":10}}`)
	result := callGuardedGetLogs(t, s, guardrail.ModeOff, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.EstimateCalls.Load() != 0 || s.QueryCalls.Load() == 0 {
		t.Fatalf("isError=%v estimates=%d queries=%d", result.IsError, s.EstimateCalls.Load(), s.QueryCalls.Load())
	}
}

func TestGetLogsGuardrailDefaultModeSkipsEstimate(t *testing.T) {
	s := newGuardrailServer(t, http.StatusNotFound, `404 page not found`)
	result := callGuardedGetLogs(t, s, guardrail.DefaultMode, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 60})

	if result.IsError || s.EstimateCalls.Load() != 0 || s.QueryCalls.Load() == 0 {
		t.Fatalf("isError=%v estimates=%d queries=%d", result.IsError, s.EstimateCalls.Load(), s.QueryCalls.Load())
	}
	if h := guardrailtest.EnvelopeHint(t, result, "query_guardrail"); h != "" {
		t.Fatalf("default mode added a guardrail hint: %q", h)
	}
}

func callGuardedGetServiceLogs(t *testing.T, s *guardrailtest.Server, mode string, args GetServiceLogsArgs) *mcp.CallToolResult {
	t.Helper()
	result, _, err := NewGetServiceLogsHandler(s.Client(), guardrailConfig(s.URL, mode))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func TestGetServiceLogsGuardrailEnforceRejectNeverQueries(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetServiceLogs(t, s, guardrail.ModeEnforce, GetServiceLogsArgs{ServiceName: "api", LookbackMinutes: 60})

	if n := s.QueryCalls.Load(); n != 0 {
		t.Fatalf("upstream query called %d times on an enforce reject", n)
	}
	if !result.IsError || guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonScanBudgetExceeded+"] rejected") == "" {
		t.Fatalf("isError=%v hints=%v", result.IsError, utils.GetResultEnvelope(t, result).Hints)
	}
}

func TestGetServiceLogsGuardrailShadowRunsAndHints(t *testing.T) {
	s := newGuardrailServer(t, http.StatusOK, `{"data":{"bytes":5001}}`)
	result := callGuardedGetServiceLogs(t, s, guardrail.ModeShadow, GetServiceLogsArgs{ServiceName: "api", LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.QueryCalls.Load() == 0 {
		t.Fatalf("shadow blocked the query: isError=%v queries=%d", result.IsError, s.QueryCalls.Load())
	}
	if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonScanBudgetExceeded+"] would be rejected") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
	if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonRangeTooWide+"]") != "" {
		t.Error("a service-scoped query was held to the unscoped range cap")
	}
}

func TestGetLogsGuardrailScopedFilterSkipsRangeCapWhenEstimateUnavailable(t *testing.T) {
	s := newGuardrailServer(t, http.StatusNotFound, `404 page not found`)
	result := callGuardedGetLogs(t, s, guardrail.ModeEnforce, GetLogsArgs{LogjsonQuery: serviceErrorFilter, LookbackMinutes: twoDaysMinutes})

	if result.IsError || s.QueryCalls.Load() == 0 {
		t.Fatalf("scoped query blocked without an estimate: isError=%v queries=%d", result.IsError, s.QueryCalls.Load())
	}
	if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonRangeTooWide+"]") != "" {
		t.Fatalf("range cap applied to a scoped query: %v", utils.GetResultEnvelope(t, result).Hints)
	}
}

func TestGetLogsGuardrailEstimateUnavailableStillRuns(t *testing.T) {
	s := newGuardrailServer(t, http.StatusNotFound, `404 page not found`)
	result := callGuardedGetLogs(t, s, guardrail.ModeEnforce, GetLogsArgs{LogjsonQuery: rawErrorFilter, LookbackMinutes: 60})

	if result.IsError || s.QueryCalls.Load() == 0 {
		t.Fatalf("estimate failure blocked the query: isError=%v queries=%d", result.IsError, s.QueryCalls.Load())
	}
	if guardrailtest.EnvelopeHint(t, result, "query_guardrail["+guardrail.ReasonEstimateUnavailable+"]") == "" {
		t.Fatalf("hints = %v", utils.GetResultEnvelope(t, result).Hints)
	}
}
