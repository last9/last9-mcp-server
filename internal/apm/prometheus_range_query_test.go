package apm

import (
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
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callPromRangeQuery(t *testing.T, body string) *mcp.CallToolResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	result, _, err := NewPromqlRangeQueryHandler(server.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, PromqlRangeQueryArgs{
		Query: "up", LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func promSeries(n int, labelBytes int) string {
	series := make([]string, n)
	for i := range series {
		series[i] = fmt.Sprintf(`{"metric":{"__name__":"up","pad":"%s"},"values":[[1700000000,"1"]]}`, strings.Repeat("x", labelBytes))
	}
	return "[" + strings.Join(series, ",") + "]"
}

func TestPromRangeQueryKeepsBodyAndMirrorsSeries(t *testing.T) {
	body := promSeries(2, 3)
	result := callPromRangeQuery(t, body)

	if len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != body {
		t.Fatalf("content = %#v, want the upstream body unchanged and no envelope block", result.Content)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	var structured struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.Unmarshal(encoded, &structured); err != nil || len(structured.Result) != 2 {
		t.Fatalf("structuredContent = %s, want the two series", encoded)
	}
	if env := utils.GetResultEnvelope(t, result); !env.IsEmpty() {
		t.Fatalf("envelope = %+v, want empty", env)
	}
}

func TestPromRangeQueryEmptyResultHintsLabelDiscovery(t *testing.T) {
	result := callPromRangeQuery(t, `[]`)

	env := utils.GetResultEnvelope(t, result)
	if len(env.Hints) != 1 || !strings.Contains(env.Hints[0], "prometheus_labels") || !strings.Contains(env.Hints[0], "prometheus_label_values") {
		t.Fatalf("hints = %v, want one pointing at label discovery", env.Hints)
	}
}

func TestPromRangeQueryTrimsOversizedResponse(t *testing.T) {
	result := callPromRangeQuery(t, promSeries(40, 4000))

	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > utils.MaxResultTextBytes {
		t.Fatalf("payload text is %d bytes, over budget", len(text))
	}
	var kept []map[string]any
	if err := json.Unmarshal([]byte(text), &kept); err != nil {
		t.Fatalf("trimmed text is not a JSON array: %v", err)
	}
	if env := utils.GetResultEnvelope(t, result); len(kept) == 0 || env.TrimmedItems != 40-len(kept) {
		t.Fatalf("kept %d, trimmed_items %d, want the two to sum to 40", len(kept), env.TrimmedItems)
	}
}

func TestPromRangeQueryPassesThroughNonArrayBody(t *testing.T) {
	result := callPromRangeQuery(t, `not json`)

	if len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != "not json" {
		t.Fatalf("content = %#v, want the body unchanged", result.Content)
	}
	if result.StructuredContent != nil {
		t.Fatalf("structuredContent = %#v, want none for an unparseable body", result.StructuredContent)
	}
}
