package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServiceOperationsSummary_EnvRegexQuoting(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		wantMatch  string
		wantReject string
	}{
		{name: "empty env matches anything", env: "", wantMatch: "anything-env", wantReject: ""},
		{name: "plain env", env: "prod", wantMatch: "prod", wantReject: "staging"},
		{name: "dotted env", env: "prod.v1", wantMatch: "prod.v1", wantReject: "prodXv1"},
		{name: "bracketed env", env: "prod[blue]", wantMatch: "prod[blue]", wantReject: "prodb"},
		{name: "anchored env", env: "^production$", wantMatch: "production", wantReject: "production-eu"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu       sync.Mutex
				captured []string
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
					return
				}
				if err := json.Unmarshal(b, &body); err != nil {
					t.Errorf("failed to unmarshal request body %q: %v", b, err)
					return
				}
				mu.Lock()
				captured = append(captured, body.Query)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("[]"))
			}))
			defer server.Close()

			handler := NewServiceOperationsSummaryHandler(server.Client(), apmTestConfig(server.URL))
			now := time.Now().UTC()
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, ServiceOperationsSummaryArgs{
				ServiceName:  "svc",
				StartTimeISO: now.Add(-60 * time.Minute).Format(time.RFC3339),
				EndTimeISO:   now.Format(time.RFC3339),
				Env:          tt.env,
			})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(captured) == 0 {
				t.Fatal("expected at least one query to be captured")
			}
			for _, q := range captured {
				assertEnvMatcherBehavior(t, q, tt.wantMatch, tt.wantReject)
			}
		})
	}
}

func TestServiceOperationsLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		rows := "[]"
		if strings.HasPrefix(body.Query, "sum by") && !strings.Contains(body.Query, "duration") {
			switch {
			case strings.Contains(body.Query, "trace_endpoint_count"):
				rows = `[{"metric":{"span_name":"quiet"},"value":[1,"1"]}]`
			case strings.Contains(body.Query, "messaging_system!=''"):
				rows = `[{"metric":{"span_name":"z"},"value":[1,"9"]},{"metric":{"span_name":"a"},"value":[1,"9"]}]`
			}
		}
		io.WriteString(w, rows)
	}))
	defer server.Close()
	handler := NewServiceOperationsSummaryHandler(server.Client(), apmTestConfig(server.URL))
	for _, tc := range []struct {
		limit *int
		names []string
	}{
		{nil, []string{"quiet", "z", "a"}}, {new(1), []string{"a"}}, {new(2), []string{"a", "z"}}, {new(8), []string{"a", "z", "quiet"}},
	} {
		result, _, err := handler(context.Background(), nil, ServiceOperationsSummaryArgs{ServiceName: "example", Limit: tc.limit})
		if err != nil {
			t.Fatal(err)
		}
		var got ServiceOperationsSummaryResponse
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &got); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, op := range got.Operations {
			names = append(names, op.Name)
		}
		if !slices.Equal(names, tc.names) || (tc.limit == nil) != (got.TotalOperations == nil) || got.TotalOperations != nil && *got.TotalOperations != 3 {
			t.Fatalf("unexpected response: %+v", got)
		}
	}
	before := calls.Load()
	for _, limit := range []int{0, -1} {
		_, _, err := handler(context.Background(), nil, ServiceOperationsSummaryArgs{ServiceName: "example", Limit: &limit})
		if err == nil || calls.Load() != before {
			t.Fatal("invalid limit should fail before querying")
		}
	}
}
