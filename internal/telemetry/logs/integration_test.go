package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const integrationTestTimeout = 30 * time.Second

// TestGetLogAttributesHandler_Integration_Basic verifies the MCP handler returns
// a non-empty attribute list that includes attributes beyond stream labels —
// enough for the LLM to build meaningful structured attribute filters.
func TestGetLogAttributesHandler_Integration_Basic(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetLogAttributesHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetLogAttributesArgs{})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response (first 300 chars): %.300s", text)

	if len(strings.TrimSpace(text)) == 0 {
		t.Fatal("expected non-empty response from get_log_attributes")
	}

	// Handler emits "Found N attributes in the time window ..."
	if !strings.Contains(text, "attributes") {
		t.Fatalf("expected 'attributes' in response, got: %s", text)
	}

	// With JSON parse stage, should return substantially more than 20 stream labels.
	// Count lines that look like attribute entries (non-empty, non-header lines).
	lines := strings.Split(text, "\n")
	attrCount := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && !strings.HasPrefix(trimmed, "Found") {
			attrCount++
		}
	}
	t.Logf("attribute lines counted: %d", attrCount)

	if attrCount < 30 {
		t.Errorf("expected >30 attributes (JSON parse should discover body fields), got %d", attrCount)
	}
}

// TestGetServiceLogsHandler_Integration_EnvFilter verifies that the env parameter
// scopes results to a single environment. A non-existent env must return zero logs —
// if it returns the same count as an unfiltered query, the filter is silently ignored.
func TestGetServiceLogsHandler_Integration_EnvFilter(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetServiceLogsHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	// First fetch logs without env filter to confirm there are logs for this service.
	resultAll, _, err := handler(ctx, &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "last9-mcp",
		LookbackMinutes: 60,
		Limit:           5,
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	textAll := utils.GetTextContent(t, resultAll)

	var allResp ServiceLogsResponse
	if err := json.Unmarshal([]byte(textAll), &allResp); err != nil {
		t.Fatalf("failed to parse unfiltered response: %v\n%s", err, textAll)
	}
	if allResp.Count == 0 {
		t.Skip("no logs for last9-mcp in last 60 minutes; skipping env filter test")
	}
	t.Logf("unfiltered log count: %d", allResp.Count)

	// Now fetch with a deliberately non-existent env — should return 0 logs,
	// proving the filter is actually applied (not silently ignored as the old
	// attributes['deployment_environment'] key would cause).
	ctx2, cancel2 := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel2()

	resultFiltered, _, err := handler(ctx2, &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "last9-mcp",
		LookbackMinutes: 60,
		Limit:           5,
		Env:             "this-env-does-not-exist-xyz123",
	})
	if utils.CheckAPIError(t, err) {
		return
	}
	textFiltered := utils.GetTextContent(t, resultFiltered)

	var filteredResp ServiceLogsResponse
	if err := json.Unmarshal([]byte(textFiltered), &filteredResp); err != nil {
		t.Fatalf("failed to parse filtered response: %v\n%s", err, textFiltered)
	}
	t.Logf("env-filtered (non-existent env) log count: %d", filteredResp.Count)

	// A deliberately non-existent env must return exactly zero logs. Any non-zero
	// count means the filter is not actually scoping by environment (e.g. the old
	// attributes['deployment_environment'] key that silently matched nothing and
	// fell through to all-env results).
	if filteredResp.Count != 0 {
		t.Errorf("env filter for a non-existent env returned %d logs; expected 0", filteredResp.Count)
	}
}

// TestGetServiceLogsHandler_Integration_Basic verifies the handler returns logs
// for a known active service with correct response structure.
func TestGetServiceLogsHandler_Integration_Basic(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)
	handler := NewGetServiceLogsHandler(http.DefaultClient, *cfg)

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout)
	defer cancel()

	result, _, err := handler(ctx, &mcp.CallToolRequest{}, GetServiceLogsArgs{
		ServiceName:     "last9-mcp",
		LookbackMinutes: 60,
		Limit:           10,
	})
	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)
	t.Logf("response (first 500 chars): %.500s", text)

	var resp ServiceLogsResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, text)
	}

	if resp.Service != "last9-mcp" {
		t.Errorf("expected service=last9-mcp in response, got %q", resp.Service)
	}
	if resp.StartTime == "" || resp.EndTime == "" {
		t.Errorf("expected start_time and end_time in response, got start=%q end=%q", resp.StartTime, resp.EndTime)
	}
	if resp.Count != len(resp.Logs) {
		t.Errorf("count=%d does not match len(logs)=%d", resp.Count, len(resp.Logs))
	}

	t.Logf("log count: %d", resp.Count)

	if resp.Count == 0 {
		t.Skip("no logs for last9-mcp in last 60 minutes; skipping entry field assertions")
	}

	// Each log entry must have timestamp and message.
	for i, entry := range resp.Logs {
		if entry.Timestamp == "" {
			t.Errorf("log[%d] missing timestamp", i)
		}
		if entry.Message == "" {
			t.Errorf("log[%d] missing message", i)
		}
	}
}
