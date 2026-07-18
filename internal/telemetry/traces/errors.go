package traces

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type traceUpstreamError struct {
	statusCode int
	requestID  string
	message    string
}

func (e *traceUpstreamError) Error() string {
	message := e.message
	if message == "" {
		message = traceUpstreamStatusMessage(e.statusCode)
	}
	if e.statusCode > 0 {
		message = fmt.Sprintf("Trace data request failed with HTTP %d. %s", e.statusCode, message)
	}
	if e.requestID != "" {
		message += fmt.Sprintf(" Request ID: %s.", e.requestID)
	}
	return message
}

func traceUpstreamStatusMessage(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Check the connection credentials and trace-data access."
	case http.StatusNotFound:
		return "The trace capability may be unavailable or disabled."
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return "Retry after a short delay or request a smaller time window."
	default:
		if statusCode >= http.StatusInternalServerError {
			return "The trace service is temporarily unavailable; retry later."
		}
		return "Review the tool arguments and retry."
	}
}

func newTraceHTTPError(response *http.Response) error {
	if response.Body != nil {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return &traceUpstreamError{
		statusCode: response.StatusCode,
		requestID:  traceRequestID(response.Header),
	}
}

func isTraceUpstreamError(err error) bool {
	var upstreamErr *traceUpstreamError
	return errors.As(err, &upstreamErr)
}

func newTraceTransportError() error {
	return &traceUpstreamError{message: "The trace service could not be reached; retry later."}
}

func newTraceInvalidResponseError() error {
	return &traceUpstreamError{message: "The trace service returned an invalid response; retry later."}
}

func traceRequestID(header http.Header) string {
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			if len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
				return !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.:/", r))
			}) == -1 {
				return value
			}
		}
	}
	return ""
}

func traceToolErrorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}
