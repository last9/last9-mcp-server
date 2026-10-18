package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"last9-mcp/internal/writefault"
)

func writeRequest(method string) *http.Request {
	return httptest.NewRequest(method, "http://api.invalid/rules", nil)
}

func upstreamStatus(status int) (*http.Response, error) {
	resp := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
	return resp, NewUpstreamHTTPError(resp, "create rule")
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestMarkUnknownWriteOutcomeClassifiesFailures(t *testing.T) {
	resp502, err502 := upstreamStatus(http.StatusBadGateway)
	resp504, err504 := upstreamStatus(http.StatusGatewayTimeout)
	resp500, err500 := upstreamStatus(http.StatusInternalServerError)
	resp400, err400 := upstreamStatus(http.StatusBadRequest)
	resp408, err408 := upstreamStatus(http.StatusRequestTimeout)
	ok200 := &http.Response{StatusCode: http.StatusOK}

	for _, tt := range []struct {
		name      string
		method    string
		resp      *http.Response
		err       error
		wantCause string // "" means the outcome is known
	}{
		{"client timeout", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: timeoutError{}}, "the request timed out"},
		{"context deadline", http.MethodPost, nil, fmt.Errorf("create rule: %w", context.DeadlineExceeded), "the request timed out"},
		{"connection reset", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}}, "the connection was reset"},
		{"EOF before status", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: io.EOF}, "the connection closed before the response was complete"},
		{"body cut off", http.MethodPost, ok200, fmt.Errorf("read response: %w", io.ErrUnexpectedEOF), "the connection closed before the response was complete"},
		{"gateway 502", http.MethodPost, resp502, err502, "the gateway returned HTTP 502"},
		{"gateway 504", http.MethodPost, resp504, err504, "the gateway returned HTTP 504"},
		{"undecodable 200", http.MethodPost, ok200, errors.New("decode response: invalid character"), "the API answered HTTP 200 but its response could not be read"},
		{"PUT timeout", http.MethodPut, nil, context.DeadlineExceeded, "the request timed out"},
		{"PATCH timeout", http.MethodPatch, nil, context.DeadlineExceeded, "the request timed out"},
		{"DELETE timeout", http.MethodDelete, nil, context.DeadlineExceeded, "the request timed out"},

		{"dial refused", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, ""},
		{"dial timeout", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "dial", Err: timeoutError{}}}, ""},
		{"DNS failure", http.MethodPost, nil, &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", IsTimeout: true}}}, ""},
		{"bare DNS failure", http.MethodPost, nil, &net.DNSError{Err: "no such host", IsNotFound: true}, ""},
		{"HTTP 500", http.MethodPost, resp500, err500, ""},
		{"HTTP 400", http.MethodPost, resp400, err400, ""},
		{"HTTP 408", http.MethodPost, resp408, err408, ""},
		{"undecodable GET 200", http.MethodGet, ok200, errors.New("decode response: invalid character"), ""},
		{"GET timeout", http.MethodGet, nil, context.DeadlineExceeded, ""},
		{"HEAD reset", http.MethodHead, nil, syscall.ECONNRESET, ""},
		{"GET 504", http.MethodGet, resp504, err504, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkUnknownWriteOutcome(writeRequest(tt.method), tt.resp, tt.err)
			if !errors.Is(got, tt.err) {
				t.Fatalf("marked error %v no longer wraps %v", got, tt.err)
			}
			var unknown *WriteOutcomeUnknownError
			isUnknown := errors.As(got, &unknown)
			if tt.wantCause == "" {
				if isUnknown {
					t.Fatalf("known outcome marked unknown: %q", unknown.Cause)
				}
				return
			}
			if !isUnknown {
				t.Fatalf("outcome not marked unknown: %v", got)
			}
			if unknown.Cause != tt.wantCause {
				t.Fatalf("cause = %q, want %q", unknown.Cause, tt.wantCause)
			}
			if got.Error() != tt.err.Error() {
				t.Fatalf("marking changed the message: %q, want %q", got.Error(), tt.err.Error())
			}
		})
	}
}

func TestMarkUnknownWriteOutcomeNilError(t *testing.T) {
	if err := MarkUnknownWriteOutcome(writeRequest(http.MethodPost), nil, nil); err != nil {
		t.Fatalf("nil error marked as %v", err)
	}
}

// postThroughRealClient runs one POST the way a write path does: send, then read the body.
func postThroughRealClient(t *testing.T, client *http.Client, target string) error {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return MarkUnknownWriteOutcome(req, nil, err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		return MarkUnknownWriteOutcome(req, resp, err)
	}
	return nil
}

func TestMarkUnknownWriteOutcomeOnRealTransportFaults(t *testing.T) {
	for _, tt := range []struct {
		fault     writefault.Fault
		wantCause string
	}{
		{writefault.Hang, "the request timed out"},
		{writefault.Close, "the connection closed before the response was complete"},
		{writefault.Truncated, "the connection closed before the response was complete"},
	} {
		t.Run(string(tt.fault), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writefault.Serve(w, r, tt.fault)
			}))
			defer srv.Close()
			err := postThroughRealClient(t, &http.Client{Timeout: 200 * time.Millisecond}, srv.URL)
			var unknown *WriteOutcomeUnknownError
			if !errors.As(err, &unknown) {
				t.Fatalf("%s: outcome not marked unknown: %v", tt.fault, err)
			}
			if unknown.Cause != tt.wantCause {
				t.Fatalf("cause = %q, want %q", unknown.Cause, tt.wantCause)
			}
		})
	}
}

func TestMarkUnknownWriteOutcomeRefusedConnectionIsKnown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	target := srv.URL
	srv.Close()
	err := postThroughRealClient(t, &http.Client{Timeout: time.Second}, target)
	if err == nil {
		t.Fatal("expected a dial error")
	}
	var unknown *WriteOutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("refused connection marked unknown (%q): %v", unknown.Cause, err)
	}
}
