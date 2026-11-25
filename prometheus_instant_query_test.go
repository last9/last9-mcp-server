package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/apm"
	"last9-mcp/internal/prompts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisteredPrometheusInstantQueryTimeAlias(t *testing.T) {
	var (
		requests   atomic.Int32
		timestamps = make(chan int64, 1)
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Timestamp int64 `json:"timestamp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		timestamps <- body.Timestamp
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(upstream.Close)

	cfg := testToolRegistrationConfig()
	cfg.APIBaseURL = upstream.URL
	session := connectToolFixture(t, registeredToolFixture{timeout: 5 * time.Second, tool: &mcp.Tool{
		Name:        "prometheus_instant_query",
		Annotations: readOnlyTool("Run PromQL Instant Query"),
		Description: prompts.PromqlInstantQueryDetails,
		InputSchema: apm.PromqlInstantQueryInputSchema(),
	}}, apm.NewPromqlInstantQueryHandler(upstream.Client(), cfg))

	result := callRegisteredTool(t, session, &mcp.CallToolParams{
		Name:      "prometheus_instant_query",
		Arguments: json.RawMessage(`{"query":"vector(time())","time":1735689600}`),
	})
	if result.IsError {
		t.Fatalf("time alias rejected: %s", toolResultText(result))
	}
	if got := <-timestamps; got != 1735689600 {
		t.Fatalf("upstream timestamp = %d, want 1735689600", got)
	}

	before := requests.Load()
	result = callRegisteredTool(t, session, &mcp.CallToolParams{
		Name:      "prometheus_instant_query",
		Arguments: json.RawMessage(`{"query":"vector(time())","time":1735689600,"time_iso":"2025-01-01T00:00:01Z"}`),
	})
	if !result.IsError {
		t.Fatalf("conflicting aliases accepted: %s", toolResultText(result))
	}
	if got := requests.Load(); got != before {
		t.Fatalf("conflicting aliases made %d upstream requests, want 0", got-before)
	}

	result = callRegisteredTool(t, session, &mcp.CallToolParams{
		Name:      "prometheus_instant_query",
		Arguments: json.RawMessage(`{"query":"vector(time())","time":-1.5}`),
	})
	if !result.IsError {
		t.Fatalf("negative evaluation time accepted: %s", toolResultText(result))
	}
	if got := requests.Load(); got != before {
		t.Fatalf("negative evaluation time made %d upstream requests, want 0", got-before)
	}
}
