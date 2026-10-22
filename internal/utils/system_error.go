package utils

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrorTypeMetaKey is the tool-result _meta key the instrumentation SDK reads to label
// an isError result's mcp.error.type; untagged results stay user_error.
const (
	ErrorTypeMetaKey = "error_type"
	SystemErrorType  = "system_error"
)

// systemErrorer lets a package's own upstream error type classify itself.
type systemErrorer interface {
	SystemError() bool
}

// IsSystemError reports whether err is a server or upstream failure (HTTP 5xx or 408,
// timeout, unreachable service) rather than a caller mistake.
func IsSystemError(err error) bool {
	if err == nil {
		return false
	}
	var classified systemErrorer
	if errors.As(err, &classified) {
		return classified.SystemError()
	}
	var upstream *UpstreamHTTPError
	if errors.As(err, &upstream) {
		return IsSystemStatus(upstream.StatusCode)
	}
	var netErr net.Error
	var urlErr *url.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.As(err, &urlErr)
}

// IsSystemStatus reports whether an upstream HTTP status is the upstream's failure.
func IsSystemStatus(status int) bool {
	return status == http.StatusRequestTimeout || status >= http.StatusInternalServerError
}

// ErrorResult builds a tool error result from err, tagged as a system error when
// IsSystemError says so.
func ErrorResult(err error) *mcp.CallToolResult {
	result := ToolErrorResult(err.Error())
	if IsSystemError(err) {
		result.Meta = mcp.Meta{ErrorTypeMetaKey: SystemErrorType}
	}
	return result
}
