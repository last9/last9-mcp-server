package traces

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNewTraceHTTPErrorDrainsResponseBody(t *testing.T) {
	body := &trackingReadCloser{Buffer: bytes.NewBufferString(strings.Repeat("x", 1024))}
	response := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{},
		Body:       body,
	}
	_ = newTraceHTTPError(response)
	if body.Len() != 0 {
		t.Fatalf("expected response body to be drained, %d bytes remain", body.Len())
	}
}

type trackingReadCloser struct {
	*bytes.Buffer
}

func (body *trackingReadCloser) Close() error { return nil }

func TestTraceRequestIDAllowsOnlyOpaqueIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "opaque ID", value: "req-123_ab.cd/ef:01", want: "req-123_ab.cd/ef:01"},
		{name: "rejects spaces", value: "customer name", want: ""},
		{name: "rejects structured data", value: `{"tenant":"private"}`, want: ""},
		{name: "rejects oversized value", value: strings.Repeat("a", 129), want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{"X-Request-Id": []string{test.value}}
			if got := traceRequestID(header); got != test.want {
				t.Fatalf("traceRequestID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTraceUpstreamStatusGuidance(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusUnauthorized, want: "credentials"},
		{status: http.StatusForbidden, want: "access"},
		{status: http.StatusNotFound, want: "unavailable or disabled"},
		{status: http.StatusRequestTimeout, want: "smaller time window"},
		{status: http.StatusTooManyRequests, want: "short delay"},
		{status: http.StatusInternalServerError, want: "temporarily unavailable"},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			response := &http.Response{StatusCode: test.status, Header: http.Header{}}
			message := newTraceHTTPError(response).Error()
			if !strings.Contains(message, test.want) || !strings.Contains(message, strconv.Itoa(test.status)) {
				t.Fatalf("unexpected guidance for %d: %s", test.status, message)
			}
		})
	}
}

func TestGetServiceTracesHandlerReturnsToolErrorForTransportFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "cancellation", err: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, test.err
			})}
			result, _, err := GetServiceTracesHandler(client, newTestCfg("https://upstream.invalid"))(
				context.Background(),
				&mcp.CallToolRequest{},
				GetServiceTracesArgs{ServiceName: "service", LookbackMinutes: 1},
			)
			if err != nil {
				t.Fatalf("expected tool execution error, got protocol error: %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("expected IsError=true, got %+v", result)
			}
			message := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(message, "could not be reached") || strings.Contains(message, test.err.Error()) {
				t.Fatalf("unsafe transport error: %s", message)
			}
		})
	}
}

func TestGetTracesHandlerPreservesPreflightErrors(t *testing.T) {
	cfg := newTestCfg("")
	now := time.Now().UTC()
	result, _, err := NewGetTracesHandler(http.DefaultClient, cfg)(
		context.Background(),
		&mcp.CallToolRequest{},
		GetTracesArgs{
			TracejsonQuery: []map[string]interface{}{{
				"type":  "filter",
				"query": map[string]interface{}{"$exists": []string{"ServiceName"}},
			}},
			StartTimeISO: now.Add(-time.Minute).Format(time.RFC3339),
			EndTimeISO:   now.Format(time.RFC3339),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "failed to prepare trace data request") {
		t.Fatalf("expected preflight protocol error, got result=%+v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("expected no tool result for preflight error, got %+v", result)
	}
}
