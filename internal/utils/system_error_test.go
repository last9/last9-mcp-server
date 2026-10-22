package utils

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type selfClassified bool

func (s selfClassified) Error() string     { return "self-classified" }
func (s selfClassified) SystemError() bool { return bool(s) }

func TestIsSystemError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("invalid regexp"), false},
		{"HTTP 400", &UpstreamHTTPError{StatusCode: http.StatusBadRequest}, false},
		{"HTTP 404", &UpstreamHTTPError{StatusCode: http.StatusNotFound}, false},
		{"HTTP 429", &UpstreamHTTPError{StatusCode: http.StatusTooManyRequests}, false},
		{"HTTP 408", &UpstreamHTTPError{StatusCode: http.StatusRequestTimeout}, true},
		{"HTTP 500", &UpstreamHTTPError{StatusCode: http.StatusInternalServerError}, true},
		{"HTTP 503 wrapped", fmt.Errorf("chunk 1/2 failed: %w", &UpstreamHTTPError{StatusCode: http.StatusServiceUnavailable}), true},
		{"context deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), true},
		{"client timeout", &url.Error{Op: "Post", URL: "http://x", Err: timeoutErr{}}, true},
		{"connection refused", &url.Error{Op: "Post", URL: "http://x", Err: syscall.ECONNREFUSED}, true},
		{"self-classified system", selfClassified(true), true},
		{"self-classified user", fmt.Errorf("wrapped: %w", selfClassified(false)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSystemError(tc.err); got != tc.want {
				t.Fatalf("IsSystemError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestErrorResultTagsOnlySystemErrors(t *testing.T) {
	system := ErrorResult(&UpstreamHTTPError{StatusCode: http.StatusBadGateway, message: "logs query failed with HTTP 502."})
	if !system.IsError || system.Meta[ErrorTypeMetaKey] != SystemErrorType {
		t.Fatalf("502 result = %+v, want isError tagged %s", system, SystemErrorType)
	}
	if got := system.Content[0].(*mcp.TextContent).Text; got != "logs query failed with HTTP 502." {
		t.Fatalf("502 text = %q", got)
	}

	user := ErrorResult(&UpstreamHTTPError{StatusCode: http.StatusBadRequest, message: "bad pipeline"})
	if !user.IsError || user.Meta != nil {
		t.Fatalf("400 result = %+v, want isError without meta", user)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "Client.Timeout exceeded while awaiting headers" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
