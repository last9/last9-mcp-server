package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDescribeMetricToolsets(t *testing.T) {
	for _, spec := range []string{"all", "metrics", "investigate", "logs", "traces", "alerts", "dashboards", "profiles", "grafana"} {
		for _, readOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/read-only=%t", spec, readOnly), func(t *testing.T) {
				filter := toolFilter(t, spec, readOnly, "")
				instructions, names := initializeWith(t, filter)
				want := spec == "all" || spec == "metrics" || spec == "investigate"
				if names["describe_metric"] != want || strings.Contains(instructions, "`describe_metric`") != want {
					t.Fatalf("tool or policy membership incorrect: %v", names["describe_metric"])
				}
				var buf bytes.Buffer
				if err := dumpTools(&buf, filter); err != nil {
					t.Fatal(err)
				}
				var dump struct {
					Tools []mcp.Tool `json:"tools"`
				}
				if err := json.Unmarshal(buf.Bytes(), &dump); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, tool := range dump.Tools {
					if tool.Name != "describe_metric" {
						continue
					}
					found = true
					if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
						t.Fatal("missing read-only annotation")
					}
					for _, rule := range []string{"Never apply rate()/increase() to gauges", "pre-windowed", "not exhaustive", "metric_names", "datasource"} {
						if !strings.Contains(tool.Description, rule) {
							t.Errorf("missing description rule %q", rule)
						}
					}
				}
				if found != want {
					t.Fatalf("dump membership=%t want=%t", found, want)
				}
			})
		}
	}
}

func TestDescribeMetricRegisteredCalls(t *testing.T) {
	for _, status := range []int{200, 401, 403, 501, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/prom_metadata":
					w.WriteHeader(status)
					fmt.Fprint(w, `{"status":"success","data":{"queue_depth":[{"type":"gauge","unit":"","help":"Queue size"}]}}`)
				case "/apm/labels":
					fmt.Fprint(w, `["job"]`)
				default:
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			cfg := models.Config{APIBaseURL: srv.URL, PrometheusReadURL: "https://prom.example", TokenManager: &auth.TokenManager{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)}, ToolFilter: toolFilter(t, "metrics", true, "")}
			server := newRegisterTestServer(t)
			if err := registerToolsAndInstructions(server, cfg); err != nil {
				t.Fatal(err)
			}
			session := connectRegisteredServer(t, server)
			res := callRegisteredTool(t, session, &mcp.CallToolParams{Name: "describe_metric", Arguments: map[string]any{"metric_names": []string{"queue_depth"}}})
			if status == 200 || status == 501 || status == 503 {
				if res.IsError {
					t.Fatalf("%+v", res)
				}
				text := toolResultText(res)
				want := `"type":"gauge"`
				if status == 501 {
					want = `"metadata_status":"metadata_unavailable"`
				}
				if status == 503 {
					want = `"metadata_status":"metadata_error"`
				}
				if !strings.Contains(text, want) {
					t.Fatalf("%s", text)
				}
			} else {
				assertToolError(t, res, toolErrorExpectation{contains: []string{fmt.Sprintf("HTTP %d", status)}})
			}
			calls = 0
			res = callRegisteredTool(t, session, &mcp.CallToolParams{Name: "describe_metric", Arguments: map[string]any{"metric_names": []string{"queue_depth", "bad{}"}}})
			if !res.IsError || calls != 0 {
				t.Fatal("invalid batch reached upstream")
			}
			_, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
