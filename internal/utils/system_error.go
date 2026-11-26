package utils

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrorTypeMetaKey is the _meta key the instrumentation SDK reads to label an isError result.
const (
	ErrorTypeMetaKey = last9mcp.MetaKeyErrorType
	SystemErrorType  = "system_error"
)

type systemErrorer interface {
	SystemError() bool
}

func IsSystemError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
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

func IsSystemStatus(status int) bool {
	return status == http.StatusRequestTimeout || status >= http.StatusInternalServerError
}

func ErrorResult(err error) *mcp.CallToolResult {
	result := ToolErrorResult(err.Error())
	if IsSystemError(err) {
		result.Meta = mcp.Meta{ErrorTypeMetaKey: SystemErrorType}
	}
	return result
}
