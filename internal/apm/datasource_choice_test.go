package apm

import (
	"context"
	"encoding/json"
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

func promToolsUnderTest(t *testing.T) (map[string]promToolCall, *[]string) {
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
		want := `datasource "prod" matches several datasources: prod-eu, prod-us. No query was run; retry with one of them as datasource.`
		if texts := resultTexts(res); len(texts) != 1 || texts[0] != want {
			t.Errorf("%s: text = %q, want %q", tool, texts, want)
		}
	}
	if len(*readURLs) != 0 {
		t.Errorf("ambiguous datasource reached the API %d time(s), want 0", len(*readURLs))
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
