package apm

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewEnvQueryMatchesReferenceFilter(t *testing.T) {
	for _, kind := range []string{"performance", "operations", "dependency"} {
		for _, env := range []string{"production", "prod.v1", "^production$", "^(production)$", "^(?:production)$"} {
			t.Run(kind+"/"+env, func(t *testing.T) {
				server := newApmCaptureServer(t)
				cfg := apmTestConfig(server.URL)
				var result *mcp.CallToolResult
				var err error
				start, end := "2026-01-01T11:58:00Z", "2026-01-01T12:00:00Z"
				switch kind {
				case "performance":
					result, _, err = NewServicePerformanceDetailsHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, ServicePerformanceDetailsArgs{ServiceName: "checkout", Env: env, StartTimeISO: start, EndTimeISO: end})
				case "operations":
					result, _, err = NewServiceOperationsSummaryHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, ServiceOperationsSummaryArgs{ServiceName: "checkout", Env: env, StartTimeISO: start, EndTimeISO: end})
				case "dependency":
					result, _, err = NewServiceDependencyGraphHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, ServiceDependencyGraphArgs{ServiceName: "checkout", Env: env, StartTimeISO: start, EndTimeISO: end})
				}
				if err != nil {
					t.Fatal(err)
				}
				reference, err := url.Parse(result.Meta["reference_url"].(string))
				if err != nil {
					t.Fatal(err)
				}
				var filters []string
				if err = json.Unmarshal([]byte(reference.Query().Get("filter")), &filters); err != nil {
					t.Fatal(err)
				}
				if len(filters) != 1 {
					t.Fatalf("unexpected filters: %v", filters)
				}
				catalogEnv, err := strconv.Unquote(strings.TrimPrefix(filters[0], "deployment_environment="))
				if err != nil {
					t.Fatal(err)
				}
				matcher := regexp.MustCompile(`env=~("(?:[^"\\]|\\.)*")`)
				found := false
				for _, query := range server.allQueries() {
					match := matcher.FindStringSubmatch(query)
					if match == nil {
						continue
					}
					found = true
					pattern, err := strconv.Unquote(match[1])
					if err != nil {
						t.Fatal(err)
					}
					compiled, err := regexp.Compile("^(?:" + pattern + ")$")
					if err != nil {
						t.Fatal(err)
					}
					if !compiled.MatchString(catalogEnv) {
						t.Fatalf("query env pattern %q excludes reference filter %q", pattern, catalogEnv)
					}
				}
				if !found {
					t.Fatal("no environment query captured")
				}
			})
		}
	}
}
