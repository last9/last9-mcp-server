package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
)

// WriteOutcomeUnknownError marks a failed write request that may still have been applied
// upstream: the request may have reached the server, but no definite, readable answer came
// back. Retrying it blindly can duplicate a create.
type WriteOutcomeUnknownError struct {
	Cause string
	Err   error
}

func (e *WriteOutcomeUnknownError) Error() string { return e.Err.Error() }

func (e *WriteOutcomeUnknownError) Unwrap() error { return e.Err }

// MarkUnknownWriteOutcome wraps err in *WriteOutcomeUnknownError when req writes and err
// leaves it unknown whether the server applied it. resp is the response received, or nil.
// Every other error, including failures to connect, is returned unchanged.
func MarkUnknownWriteOutcome(req *http.Request, resp *http.Response, err error) error {
	if err == nil || req == nil || !isWriteMethod(req.Method) {
		return err
	}
	cause := unknownOutcomeCause(resp, err)
	if cause == "" {
		return err
	}
	return &WriteOutcomeUnknownError{Cause: cause, Err: err}
}

func isWriteMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func unknownOutcomeCause(resp *http.Response, err error) string {
	if resp != nil && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout) {
		return fmt.Sprintf("the gateway returned HTTP %d", resp.StatusCode)
	}
	if neverConnected(err) {
		return ""
	}
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "the request timed out"
	case errors.Is(err, syscall.ECONNRESET):
		return "the connection was reset"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "the connection closed before the response was complete"
	case resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return fmt.Sprintf("the API answered HTTP %d but its response could not be read", resp.StatusCode)
	}
	return ""
}

// A request that failed to resolve or dial never left this process.
func neverConnected(err error) bool {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	return errors.As(err, &dnsErr) || (errors.As(err, &opErr) && opErr.Op == "dial")
}
