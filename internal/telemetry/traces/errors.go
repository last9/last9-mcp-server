package traces

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tracePipelineSchemaHint = `Pipeline stage schema: every stage needs "type" (filter | where | aggregate | window_aggregate). filter conditions use $and/$or/$not with field operators like $eq, $neq, $contains. Field names are exact: ServiceName, TraceId, SpanName; use attributes['name'] / resources['name'] for others. To discover attribute keys present for your scope, call get_trace_attributes_for_pipeline with your filter stages.`

var traceRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

type traceUpstreamError struct {
	statusCode int
	requestID  string
	message    string
	cause      error
}

func (e *traceUpstreamError) Error() string {
	message := e.message
	if message == "" {
		if e.statusCode > 0 {
			message = traceUpstreamStatusMessage(e.statusCode)
		} else {
			return "trace service error"
		}
	}
	if e.statusCode > 0 {
		message = fmt.Sprintf("Trace data request failed with HTTP %d. %s", e.statusCode, message)
	}
	if e.requestID != "" {
		message += fmt.Sprintf(" Request ID: %s.", e.requestID)
	}
	return message
}

func (e *traceUpstreamError) Unwrap() error {
	return e.cause
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
	status := response.StatusCode
	err := &traceUpstreamError{
		statusCode: status,
		requestID:  traceRequestID(response.Header),
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		body := readLimitedResponseBody(response.Body, 4<<10)
		err.message = fmt.Sprintf("Review the tool arguments and retry. Upstream response: %s\n\n%s", body, tracePipelineSchemaHint)
		return err
	}
	drainResponseBody(response.Body)
	return err
}

func newTraceNotFoundTraceError() error {
	return &traceUpstreamError{
		statusCode: http.StatusNotFound,
		message:    "No trace found for the given trace_id in this time window; widen the time range or verify the trace ID.",
	}
}

func newTraceAPIStatusError(status string) error {
	return &traceUpstreamError{
		message: fmt.Sprintf("The trace API returned status %q; review the tool arguments and retry.", status),
	}
}

func isTraceUpstreamError(err error) bool {
	var upstreamErr *traceUpstreamError
	return errors.As(err, &upstreamErr)
}

func newTraceTransportError(cause error) error {
	return &traceUpstreamError{
		cause:   cause,
		message: "The trace service could not be reached; retry later.",
	}
}

func newTraceInvalidResponseError(cause error) error {
	return &traceUpstreamError{
		cause:   cause,
		message: "The trace service returned an invalid response; retry later.",
	}
}

func traceRequestID(header http.Header) string {
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			if traceRequestIDPattern.MatchString(value) {
				return value
			}
		}
	}
	return ""
}

func traceToolErrorResult(err error) *mcp.CallToolResult {
	return utils.ToolErrorResult(err.Error())
}

func traceLogCause(err error) error {
	var upstreamErr *traceUpstreamError
	if errors.As(err, &upstreamErr) && upstreamErr.cause != nil {
		return upstreamErr.cause
	}
	return err
}

func drainResponseBody(body io.Reader) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 4<<10)
}

func readLimitedResponseBody(body io.Reader, limit int64) string {
	if body == nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = io.CopyN(&buf, body, limit)
	_, _ = io.CopyN(io.Discard, body, 4<<10)
	return buf.String()
}
