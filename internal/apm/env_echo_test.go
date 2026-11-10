package apm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/constants"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newEnvEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == constants.EndpointPromQueryInstant {
			_, _ = w.Write([]byte(`[{"metric":{"span_name":"GET /x","service_name":"checkout"},"value":[0,"5"]}]`))
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnvEchoNormalizedAcrossTools(t *testing.T) {
	cases := []struct {
		raw           string
		wantEnv       string
		wantRequested string
	}{
		{"production", "production", ""},
		{"^production$", "production", "^production$"},
		{"^(?:production)$", "production", "^(?:production)$"},
		{"^(production)$", "production", "^(production)$"},
		{"", ".*", ""},
		{".*", ".*", ""},
		{"^.*$", ".*", "^.*$"},
	}
	start, end := "2026-01-01T11:58:00Z", "2026-01-01T12:00:00Z"
	for _, tool := range []string{"performance", "operations", "dependency", "dbqueries"} {
		for _, c := range cases {
			t.Run(tool+"/"+c.raw, func(t *testing.T) {
				srv := newEnvEchoServer(t)
				cfg := apmTestConfig(srv.URL)
				ctx, req := context.Background(), &mcp.CallToolRequest{}
				var res *mcp.CallToolResult
				var err error
				switch tool {
				case "performance":
					res, _, err = NewServicePerformanceDetailsHandler(srv.Client(), cfg)(ctx, req, ServicePerformanceDetailsArgs{ServiceName: "checkout", Env: c.raw, StartTimeISO: start, EndTimeISO: end})
				case "operations":
					res, _, err = NewServiceOperationsSummaryHandler(srv.Client(), cfg)(ctx, req, ServiceOperationsSummaryArgs{ServiceName: "checkout", Env: c.raw, StartTimeISO: start, EndTimeISO: end})
				case "dependency":
					res, _, err = NewServiceDependencyGraphHandler(srv.Client(), cfg)(ctx, req, ServiceDependencyGraphArgs{ServiceName: "checkout", Env: c.raw, StartTimeISO: start, EndTimeISO: end})
				case "dbqueries":
					res, _, err = NewGetDatabaseQueriesHandler(srv.Client(), cfg)(ctx, req, GetDatabaseQueriesArgs{DBSystem: "redis", Env: c.raw, StartTimeISO: start, EndTimeISO: end})
				}
				if err != nil {
					t.Fatal(err)
				}
				var out struct {
					Env          string `json:"env"`
					EnvRequested string `json:"env_requested"`
					Operations   []struct {
						Env          string `json:"env"`
						EnvRequested string `json:"env_requested"`
					} `json:"operations"`
				}
				text := res.Content[0].(*mcp.TextContent).Text
				if err := json.Unmarshal([]byte(text), &out); err != nil {
					t.Fatalf("%v: %s", err, text)
				}
				if out.Env != c.wantEnv || out.EnvRequested != c.wantRequested {
					t.Errorf("env=%q env_requested=%q, want %q / %q", out.Env, out.EnvRequested, c.wantEnv, c.wantRequested)
				}
				if tool == "operations" {
					if len(out.Operations) == 0 {
						t.Fatal("expected operation rows")
					}
					for _, op := range out.Operations {
						if op.Env != c.wantEnv || op.EnvRequested != "" {
							t.Errorf("row env=%q env_requested=%q, want %q / empty", op.Env, op.EnvRequested, c.wantEnv)
						}
					}
				}
				if c.wantRequested == "" && strings.Contains(text, "env_requested") {
					t.Errorf("env_requested must be omitted: %s", text)
				}
			})
		}
	}
}
