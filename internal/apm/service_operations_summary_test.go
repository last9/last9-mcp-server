package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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
