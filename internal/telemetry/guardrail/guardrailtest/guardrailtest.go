// Package guardrailtest holds the fake upstream and limits that the log and
// trace tool tests run the query guardrail against.
package guardrailtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// Server answers the estimate route with estimate (status estimateStatus) and
// every other route with queryBody, counting each.
type Server struct {
	*httptest.Server
	EstimateCalls atomic.Int32
	QueryCalls    atomic.Int32
}

func NewServer(t *testing.T, estimateStatus int, estimate, queryBody string) *Server {
	t.Helper()
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/estimate") {
			s.EstimateCalls.Add(1)
			w.WriteHeader(estimateStatus)
			_, _ = w.Write([]byte(estimate))
			return
		}
		s.QueryCalls.Add(1)
		_, _ = w.Write([]byte(queryBody))
	}))
	t.Cleanup(s.Close)
	return s
}

// Limits are small enough for a stub estimate to cross each threshold: scope
// at 1000 bytes, per-query budget at 5000, range cap at 24h. The session
// budget stays off so tests never charge the process-wide session usage.
func Limits(mode string) models.QueryGuardrail {
	return models.QueryGuardrail{
		Mode:                mode,
		RangeCap:            24 * time.Hour,
		ScopeThresholdBytes: 1000,
		QueryBudgetBytes:    5000,
	}
}

// EnvelopeHint returns the result envelope's first hint starting with prefix,
// or "" when there is none.
func EnvelopeHint(t *testing.T, result *mcp.CallToolResult, prefix string) string {
	t.Helper()
	for _, h := range utils.GetResultEnvelope(t, result).Hints {
		if strings.HasPrefix(h, prefix) {
			return h
		}
	}
	return ""
}
