package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	scanCauseTimeout = "timeout"
	scanCauseFailed  = "scan_failed"
)

var getLogsScanSteps = []string{
	"add a selective `$contains` or `$eq` filter stage before any `parse` or `aggregate` stage, so fewer lines are parsed",
	"shorten the window, for example to 1h, and widen it once the narrowed query succeeds",
}

var logPatternsScanSteps = []string{
	"add `body_filters` with a distinctive message substring, so fewer lines are scanned",
	"shorten the window, for example to 1h",
	"lower `sample_size` (default 1000)",
}

// scanErrorResult returns a log scan that timed out or failed upstream as an
// error result whose l9_guidance names the cause and how to shrink the scan.
// Any other error returns nil and stays the caller's to report.
func scanErrorResult(err error, steps []string) *mcp.CallToolResult {
	cause, message := classifyScanError(err)
	if cause == "" {
		return nil
	}
	text, marshalErr := json.Marshal(map[string]any{
		"error": err.Error(),
		"l9_guidance": map[string]any{
			"cause":      cause,
			"message":    message,
			"next_steps": append(append([]string(nil), steps...), "do not retry the same request unchanged"),
		},
	})
	if marshalErr != nil {
		return nil
	}
	result := utils.ErrorResult(err)
	result.Content = []mcp.Content{&mcp.TextContent{Text: string(text)}}
	return result
}

func classifyScanError(err error) (cause, message string) {
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &upstream) {
		switch status := upstream.StatusCode; {
		case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
			return scanCauseTimeout, scanTimeoutMessage
		case status >= http.StatusInternalServerError:
			return scanCauseFailed, fmt.Sprintf("The log backend failed the scan with HTTP %d. Over a wide window this usually means the scan read too many lines before a selective filter.", status)
		}
		return "", ""
	}
	var netErr net.Error
	if errors.Is(err, utils.ErrLogSearchTimeout) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) {
		return scanCauseTimeout, scanTimeoutMessage
	}
	return "", ""
}

const scanTimeoutMessage = "The log scan timed out before returning. Wide windows that parse, aggregate or sample many lines before a selective filter are the usual cause."
