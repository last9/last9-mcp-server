package apm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sparseLookbackMinutes bounds the single retry of an empty metric-scoped label lookup;
// four days covers metrics that report once a day (e.g. CloudWatch S3 storage metrics).
const sparseLookbackMinutes = 4 * 24 * 60

var (
	namedSelector     = regexp.MustCompile(`^\s*[a-zA-Z_:][a-zA-Z0-9_:]*\s*(\{[^{}]*\})?\s*$`)
	bareSelector      = regexp.MustCompile(`^\s*\{[^{}]*\}\s*$`)
	nameEqualsMatcher = regexp.MustCompile("__name__\\s*=\\s*[\"'`]")
)

// isMetricScopedSelector reports whether query is a single series selector naming one
// metric, by name or an equality __name__ matcher. Only then is an empty result worth widening.
func isMetricScopedSelector(query string) bool {
	return namedSelector.MatchString(query) ||
		(bareSelector.MatchString(query) && nameEqualsMatcher.MatchString(query))
}

// labelLookup is one prometheus_labels or prometheus_label_values call; fetch runs it over [start, end].
// field names the key the API wraps its list in (e.g. {"labels":[...]}); empty means a bare array.
type labelLookup struct {
	op         string
	noun       string
	field      string
	matchQuery string
	fetch      func(start, end int64) (*http.Response, error)
}

// labelWindow is the caller's resolved time range; isDefault marks one the caller left unset.
type labelWindow struct {
	start, end int64
	isDefault  bool
}

func resolveLabelWindow(startTimeISO, endTimeISO string, lookbackMinutes float64) (labelWindow, error) {
	start, end, err := resolveTimeRange(startTimeISO, endTimeISO, lookbackMinutes)
	if err != nil {
		return labelWindow{}, err
	}
	isDefault := startTimeISO == "" && endTimeISO == "" && lookbackMinutes == 0
	return labelWindow{start: start, end: end, isDefault: isDefault}, nil
}

// run queries the caller's window. A metric-scoped lookup that comes back empty over
// the default window is retried once over sparseLookbackMinutes, with a note saying so;
// an explicit window is never widened.
func (l labelLookup) run(w labelWindow) (*mcp.CallToolResult, any, error) {
	body, err := l.read(w.start, w.end)
	var upstreamErr *utils.UpstreamHTTPError
	if errors.As(err, &upstreamErr) {
		return utils.ToolErrorResult(err.Error()), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	if !w.isDefault || !isMetricScopedSelector(l.matchQuery) || resultLen(body, l.field) != 0 {
		return textResult(body), nil, nil
	}

	wide, err := l.read(w.end-sparseLookbackMinutes*60, w.end)
	if err != nil {
		return textResult(body, l.emptyNote("retrying over the last 4 days failed: "+err.Error())), nil, nil
	}
	n := resultLen(wide, l.field)
	if n == 0 {
		return textResult(body, fmt.Sprintf("No %s in the last %d minutes or the last 4 days.", l.noun, utils.DefaultLookbackMinutes)), nil, nil
	}
	found := fmt.Sprintf("found %d", n)
	if n < 0 {
		found = "found a non-empty result"
	}
	return textResult(wide, l.emptyNote(found+" over the last 4 days (metric reports sparsely). Showing the wider window.")), nil, nil
}

func (l labelLookup) emptyNote(outcome string) string {
	return fmt.Sprintf("No %s in the last %d minutes; %s", l.noun, utils.DefaultLookbackMinutes, outcome)
}

func (l labelLookup) read(start, end int64) ([]byte, error) {
	httpResp, err := l.fetch(start, end)
	if err != nil {
		return nil, err
	}
	if httpResp == nil {
		return nil, fmt.Errorf("received nil response from Prometheus")
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, promErr(httpResp, l.op)
	}
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return data, nil
}

// resultLen returns the length of the list in body, a JSON array or an object holding
// it under field, 0 for null, and -1 for anything else, so an unexpected shape is
// never mistaken for an empty result.
func resultLen(body []byte, field string) int {
	if n := jsonArrayLen(body); n >= 0 || field == "" {
		return n
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return -1
	}
	return jsonArrayLen(envelope[field]) // a missing field is nil, which fails to decode: -1
}

func jsonArrayLen(body []byte) int {
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return -1
	}
	return len(items)
}

func textResult(body []byte, notes ...string) *mcp.CallToolResult {
	content := []mcp.Content{&mcp.TextContent{Text: string(body)}}
	for _, n := range notes {
		content = append(content, &mcp.TextContent{Text: n})
	}
	return &mcp.CallToolResult{Content: content}
}
