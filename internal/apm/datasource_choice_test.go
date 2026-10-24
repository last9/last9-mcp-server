package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type promToolCall func(ctx context.Context, datasource string) (*mcp.CallToolResult, error)

func promToolsUnderTest(t *testing.T, extra ...models.DatasourceInfo) (map[string]promToolCall, *[]string) {
	t.Helper()
	var readURLs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ReadURL string `json:"read_url"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		readURLs = append(readURLs, body.ReadURL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prod-us.example.com/prom",
		PrometheusUsername: "prod-us-user",
		PrometheusPassword: "prod-us-pass",
		Region:             "us-east-1",
		Datasources: []models.DatasourceInfo{
			{Name: "prod-us", ReadURL: "https://prod-us.example.com/prom", Username: "u", Password: "p", Region: "us-east-1", IsDefault: true},
			{Name: "prod-eu", ReadURL: "https://prod-eu.example.com/prom", Username: "u", Password: "p", Region: "eu-west-1"},
			{Name: "staging", ReadURL: "https://staging.example.com/prom", Username: "u", Password: "p", Region: "ap-south-1"},
		},
		TokenManager: &auth.TokenManager{AccessToken: "mock-token", ExpiresAt: time.Now().Add(time.Hour)},
	}
	cfg.Datasources = append(cfg.Datasources, extra...)
	client := server.Client()
	req := &mcp.CallToolRequest{}
	tools := map[string]promToolCall{
		"prometheus_range_query": func(ctx context.Context, ds string) (*mcp.CallToolResult, error) {
			res, _, err := NewPromqlRangeQueryHandler(client, cfg)(ctx, req, PromqlRangeQueryArgs{Query: "up", LookbackMinutes: 5, Datasource: ds})
			return res, err
		},
		"prometheus_instant_query": func(ctx context.Context, ds string) (*mcp.CallToolResult, error) {
			res, _, err := NewPromqlInstantQueryHandler(client, cfg)(ctx, req, PromqlInstantQueryArgs{Query: "up", Datasource: ds})
			return res, err
		},
		"prometheus_label_values": func(ctx context.Context, ds string) (*mcp.CallToolResult, error) {
			res, _, err := NewPromqlLabelValuesHandler(client, cfg)(ctx, req, PromqlLabelValuesArgs{MatchQuery: "up", Label: "job", LookbackMinutes: 5, Datasource: ds})
			return res, err
		},
		"prometheus_labels": func(ctx context.Context, ds string) (*mcp.CallToolResult, error) {
			res, _, err := NewPromqlLabelsHandler(client, cfg)(ctx, req, PromqlLabelsArgs{MatchQuery: "up", LookbackMinutes: 5, Datasource: ds})
			return res, err
		},
	}
	return tools, &readURLs
}

func resultTexts(res *mcp.CallToolResult) []string {
	var out []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return out
}

func TestPromToolsResolveDatasource(t *testing.T) {
	tests := []struct {
		name        string
		datasource  string
		wantReadURL string
		wantNote    string
	}{
		{"exact name, no note", "staging", "https://staging.example.com/prom", ""},
		{"near miss uses closest match", "stagign", "https://staging.example.com/prom", `Using datasource "staging" (closest match to "stagign").`},
		{"case folded", "Prod-EU", "https://prod-eu.example.com/prom", `Using datasource "prod-eu" (closest match to "Prod-EU").`},
		{"omitted uses default and says so", "", "https://prod-us.example.com/prom", `Using datasource "prod-us" (default).`},
		{"blank treated as omitted", "  ", "https://prod-us.example.com/prom", `Using datasource "prod-us" (default).`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, readURLs := promToolsUnderTest(t)
			for tool, call := range tools {
				*readURLs = nil
				res, err := call(context.Background(), tt.datasource)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", tool, err)
				}
				if res.IsError {
					t.Fatalf("%s: IsError set: %v", tool, resultTexts(res))
				}
				if len(*readURLs) != 1 || (*readURLs)[0] != tt.wantReadURL {
					t.Errorf("%s: read_urls = %v, want [%s]", tool, *readURLs, tt.wantReadURL)
				}
				texts := resultTexts(res)
				if texts[0] != "[]" {
					t.Errorf("%s: first block = %q, want the upstream body", tool, texts[0])
				}
				var note string
				if len(texts) > 1 {
					note = texts[1]
				}
				if len(texts) > 2 || note != tt.wantNote {
					t.Errorf("%s: note blocks = %q, want %q", tool, texts[1:], tt.wantNote)
				}
			}
		})
	}
}

func TestPromToolsAmbiguousDatasourceReturnsCandidates(t *testing.T) {
	tools, readURLs := promToolsUnderTest(t)
	for tool, call := range tools {
		res, err := call(context.Background(), "prod")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tool, err)
		}
		if res.IsError {
			t.Errorf("%s: IsError set, want a recoverable result", tool)
		}
		want := `datasource "prod" is not an exact name. No query was run; retry with one of these as datasource: prod-eu, prod-us.`
		if texts := resultTexts(res); len(texts) != 1 || texts[0] != want {
			t.Errorf("%s: text = %q, want %q", tool, texts, want)
		}
	}
	if len(*readURLs) != 0 {
		t.Errorf("ambiguous datasource reached the API %d time(s), want 0", len(*readURLs))
	}
}

func TestPromToolsDistantDatasourceIsOnlyOffered(t *testing.T) {
	tools, readURLs := promToolsUnderTest(t)
	for tool, call := range tools {
		res, err := call(context.Background(), "stagingxx")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tool, err)
		}
		want := `datasource "stagingxx" is not an exact name. No query was run; retry with one of these as datasource: staging.`
		if texts := resultTexts(res); res.IsError || len(texts) != 1 || texts[0] != want {
			t.Errorf("%s: isError=%v text = %q, want %q", tool, res.IsError, texts, want)
		}
	}
	if len(*readURLs) != 0 {
		t.Errorf("offered datasource reached the API %d time(s), want 0", len(*readURLs))
	}
}

func TestPromToolsNeverSwapRegionCode(t *testing.T) {
	tests := []struct{ configured, requested, available string }{
		{"canarysa", "canaryca", "canarysa, prod-eu, prod-us, staging"},
		{"telemetryuk", "telemetryus", "prod-eu, prod-us, staging, telemetryuk"},
		{"telemetryus", "telemetryuk", "prod-eu, prod-us, staging, telemetryus"},
	}
	for _, tt := range tests {
		t.Run(tt.requested, func(t *testing.T) {
			tools, readURLs := promToolsUnderTest(t, models.DatasourceInfo{Name: tt.configured, ReadURL: "https://" + tt.configured + ".example.com/prom", Username: "u", Password: "p", Region: "us-east-1"})
			for tool, call := range tools {
				_, err := call(context.Background(), tt.requested)
				want := fmt.Sprintf(`datasource %q not found; available: %s`, tt.requested, tt.available)
				if err == nil || err.Error() != want {
					t.Errorf("%s: error = %v, want %q", tool, err, want)
				}
			}
			if len(*readURLs) != 0 {
				t.Errorf("region-swapped datasource reached the API %d time(s), want 0", len(*readURLs))
			}
		})
	}
}

func TestPromToolsOfferUnlistedRegionCode(t *testing.T) {
	tools, readURLs := promToolsUnderTest(t, models.DatasourceInfo{Name: "telemetryua", ReadURL: "https://telemetryua.example.com/prom", Username: "u", Password: "p", Region: "us-east-1"})
	for tool, call := range tools {
		res, err := call(context.Background(), "telemetryus")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tool, err)
		}
		want := `datasource "telemetryus" is not an exact name. No query was run; retry with one of these as datasource: telemetryua.`
		if texts := resultTexts(res); res.IsError || len(texts) != 1 || texts[0] != want {
			t.Errorf("%s: isError=%v text = %q, want %q", tool, res.IsError, texts, want)
		}
	}
	if len(*readURLs) != 0 {
		t.Errorf("offered datasource reached the API %d time(s), want 0", len(*readURLs))
	}
}

func TestPromToolsUnknownDatasourceListsAvailable(t *testing.T) {
	tools, readURLs := promToolsUnderTest(t)
	for tool, call := range tools {
		_, err := call(context.Background(), "billing")
		if err == nil {
			t.Fatalf("%s: expected an error for an unknown datasource", tool)
		}
		want := `datasource "billing" not found; available: prod-eu, prod-us, staging`
		if err.Error() != want {
			t.Errorf("%s: error = %q, want %q", tool, err.Error(), want)
		}
	}
	if len(*readURLs) != 0 {
		t.Errorf("unknown datasource reached the API %d time(s), want 0", len(*readURLs))
	}
}

func TestAmbiguousDatasourceCountsOmittedCandidates(t *testing.T) {
	cfg := models.Config{}
	for _, n := range []string{"web-1", "web-2", "web-3", "web-4", "web-5", "web-6", "web-7"} {
		cfg.Datasources = append(cfg.Datasources, models.DatasourceInfo{Name: n, ReadURL: "https://" + n})
	}
	choice, err := chooseDatasource(cfg, "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := ambiguousDatasourceResult("web", choice)
	want := `datasource "web" is not an exact name. No query was run; retry with one of these as datasource: web-1, web-2, web-3, web-4, web-5 and 2 more.`
	if texts := resultTexts(res); len(texts) != 1 || texts[0] != want {
		t.Errorf("text = %q, want %q", texts, want)
	}
}

func TestUnknownDatasourceErrorShortensLongInput(t *testing.T) {
	cfg := models.Config{Datasources: []models.DatasourceInfo{{Name: "prod-us", ReadURL: "https://prod-us"}}}
	_, err := chooseDatasource(cfg, strings.Repeat("z", 10_000))
	if err == nil {
		t.Fatal("expected an error for an unknown datasource")
	}
	want := `datasource "` + strings.Repeat("z", 100) + `…" not found; available: prod-us`
	if err.Error() != want {
		t.Errorf("error = %q (%d bytes), want %q", err.Error()[:120], len(err.Error()), want)
	}
}

func TestClosestDatasourceWithoutPrometheusConfigSaysItWasSubstituted(t *testing.T) {
	cfg := models.Config{Datasources: []models.DatasourceInfo{{Name: "staging"}}}
	_, err := chooseDatasource(cfg, "stagign")
	want := `datasource "staging" is missing required Prometheus configuration (closest match to "stagign")`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestChooseDatasourceWithoutCachedList(t *testing.T) {
	cfg := models.Config{PrometheusReadURL: "https://configured.example.com/prom", DatasourceName: "configured"}
	choice, err := chooseDatasource(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice.cfg.PrometheusReadURL != cfg.PrometheusReadURL {
		t.Errorf("read_url = %q, want the configured one", choice.cfg.PrometheusReadURL)
	}
	if want := `Using datasource "configured" (default).`; choice.note != want {
		t.Errorf("note = %q, want %q", choice.note, want)
	}

	choice, err = chooseDatasource(models.Config{}, "")
	if err != nil || choice.note != "" {
		t.Errorf("no known default: note = %q, err = %v; want no note, no error", choice.note, err)
	}
	if strings.Contains(choice.note, `""`) {
		t.Errorf("note names an empty datasource: %q", choice.note)
	}
}
