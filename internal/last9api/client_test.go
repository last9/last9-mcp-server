package last9api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
	"last9-mcp/internal/writefault"
)

func testCfg(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		Region:     "ap-south-1",
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

func TestDo_SetsHeadersAndEncodesBody(t *testing.T) {
	var gotPath, gotAuth, gotAccept, gotUA, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-LAST9-API-TOKEN")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	var out struct {
		OK bool `json:"ok"`
	}
	err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodPost, Path: "/thing", Body: map[string]any{"a": 1}}, &out)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotPath != "/thing" {
		t.Errorf("path = %q, want /thing", gotPath)
	}
	if gotAuth != "Bearer mock-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotAccept != "application/json" || gotCT != "application/json" {
		t.Errorf("accept = %q, content-type = %q", gotAccept, gotCT)
	}
	if gotUA == "" {
		t.Error("User-Agent not set")
	}
	if gotBody["a"] != float64(1) {
		t.Errorf("body = %v", gotBody)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
}

func TestDo_PreservesTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/things/"}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotPath != "/things/" {
		t.Errorf("path = %q, want /things/ (trailing slash is significant)", gotPath)
	}
}

func TestDo_RegionHeaderAndQuery(t *testing.T) {
	var gotRegionHeader, gotRegionQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRegionHeader = r.Header.Get("region")
		gotRegionQuery = r.URL.Query().Get("region")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))

	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil, WithRegionHeader()); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotRegionHeader != "ap-south-1" || gotRegionQuery != "" {
		t.Errorf("header = %q, query = %q; want header only", gotRegionHeader, gotRegionQuery)
	}

	gotRegionHeader, gotRegionQuery = "", ""
	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil,
		WithRegionQuery(), WithQuery(url.Values{"k": {"v"}})); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotRegionQuery != "ap-south-1" || gotRegionHeader != "" {
		t.Errorf("header = %q, query = %q; want query only", gotRegionHeader, gotRegionQuery)
	}
}

func TestDo_MissingTokenFailsBeforeRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := testCfg(srv.URL)
	cfg.TokenManager = &auth.TokenManager{AccessToken: "", ExpiresAt: time.Now().Add(time.Hour)}
	c := NewClient(srv.Client(), cfg)

	err := c.Do(context.Background(), "test op", Request{Method: http.MethodGet, Path: "/a"}, nil)
	if err == nil {
		t.Fatal("want error for empty access token")
	}
	if called {
		t.Error("request was sent despite an empty access token")
	}
}

func TestDo_MissingRegionFailsWhenRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := testCfg(srv.URL)
	cfg.Region = ""
	c := NewClient(srv.Client(), cfg)

	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil, WithRegionHeader()); err == nil {
		t.Fatal("want error when region is required but empty")
	}
}

func TestDo_MaxResponseBytes(t *testing.T) {
	const body = `{"ok":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))

	for _, tc := range []struct {
		name    string
		limit   int64
		wantErr bool
	}{
		{"below body size", int64(len(body)) - 1, true},
		{"at body size", int64(len(body)), false},
		{"above body size", int64(len(body)) + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				OK bool `json:"ok"`
			}
			err := c.Do(context.Background(), "test op",
				Request{Method: http.MethodGet, Path: "/a"}, &out, WithMaxResponseBytes(tc.limit))
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "test op") {
					t.Fatalf("want an oversized-response error naming the op, got %v", err)
				}
				var tooLarge *ResponseTooLargeError
				if !errors.As(err, &tooLarge) || tooLarge.MaxBytes != tc.limit {
					t.Fatalf("want a *ResponseTooLargeError with MaxBytes %d, got %#v", tc.limit, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if !out.OK {
				t.Error("response not decoded")
			}
		})
	}
}

func TestDo_MapsNon2xxToUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	err := c.Do(context.Background(), "database query",
		Request{Method: http.MethodGet, Path: "/a"}, nil)
	if err == nil {
		t.Fatal("want error for HTTP 429")
	}
	if !strings.Contains(err.Error(), "database query") {
		t.Errorf("error does not name the operation: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error does not carry the status: %v", err)
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDo_EmptySuccessBodyLeavesOutUntouched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))
	out := struct{ A string }{A: "keep"}
	if err := c.Do(context.Background(), "op", Request{Method: http.MethodDelete, Path: "/x"}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out.A != "keep" {
		t.Errorf("out modified: %+v", out)
	}
}

func TestDo_GarbageSuccessBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))
	var out map[string]any
	err := c.Do(context.Background(), "op", Request{Method: http.MethodGet, Path: "/x"}, &out)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestDo_RequiredBodyRejectsEmptyAndTrailingData(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"valid", http.StatusOK, `{"a":"x"}` + "\n", ""},
		{"empty_200", http.StatusOK, "", "empty response body"},
		{"empty_204", http.StatusNoContent, "", "empty response body"},
		{"trailing_garbage", http.StatusOK, `{"a":"x"}broken`, "decode response"},
		{"second_value", http.StatusOK, `{"a":"x"}{"a":"y"}`, "decode response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := NewClient(srv.Client(), testCfg(srv.URL))
			var out struct{ A string }
			err := c.Do(context.Background(), "op", Request{Method: http.MethodGet, Path: "/x"}, &out, WithRequiredBody())
			if tc.wantErr == "" {
				if err != nil || out.A != "x" {
					t.Fatalf("want decoded body, got out=%+v err=%v", out, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDo_TransportEOFIsError(t *testing.T) {
	hc := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { return nil, io.EOF })}
	c := NewClient(hc, testCfg("http://unused.invalid"))
	var out json.RawMessage
	if err := c.Do(context.Background(), "op", Request{Method: http.MethodDelete, Path: "/x"}, &out); err == nil {
		t.Fatal("transport EOF returned nil error")
	}
}

func TestDo_MarksWriteFaultsAsUnknownOutcome(t *testing.T) {
	for _, fault := range writefault.All {
		for _, out := range []any{&json.RawMessage{}, nil} {
			t.Run(fmt.Sprintf("%s/decode=%v", fault, out != nil), func(t *testing.T) {
				var requests atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					writefault.Serve(w, r, fault)
				}))
				defer srv.Close()
				c := NewClient(&http.Client{Timeout: 200 * time.Millisecond}, testCfg(srv.URL))

				err := c.Do(context.Background(), "create thing", Request{Method: http.MethodPost, Path: "/things/", Body: map[string]any{}}, out)
				var unknown *utils.WriteOutcomeUnknownError
				if !errors.As(err, &unknown) {
					t.Fatalf("outcome not marked unknown: %v", err)
				}
				if !strings.Contains(err.Error(), "create thing") {
					t.Errorf("error does not name the operation: %v", err)
				}
				if n := requests.Load(); n != 1 {
					t.Errorf("server saw %d requests, want exactly 1", n)
				}
			})
		}
	}
}

func TestDo_MarksGatewayStatusOnWriteOnly(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError} {
		for _, method := range []string{http.MethodPut, http.MethodGet} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
				}))
				defer srv.Close()
				c := NewClient(srv.Client(), testCfg(srv.URL))
				err := c.Do(context.Background(), "op", Request{Method: method, Path: "/x"}, nil)
				var upstream *utils.UpstreamHTTPError
				if !errors.As(err, &upstream) || upstream.StatusCode != status {
					t.Fatalf("want UpstreamHTTPError %d, got %v", status, err)
				}
				var unknown *utils.WriteOutcomeUnknownError
				want := method == http.MethodPut && status != http.StatusInternalServerError
				if got := errors.As(err, &unknown); got != want {
					t.Fatalf("marked unknown = %v, want %v (%v)", got, want, err)
				}
			})
		}
	}
}

func TestDo_ReadFaultIsNotUnknownWriteOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writefault.Serve(w, r, writefault.Close)
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))
	err := c.Do(context.Background(), "op", Request{Method: http.MethodGet, Path: "/x"}, nil)
	var unknown *utils.WriteOutcomeUnknownError
	if err == nil || errors.As(err, &unknown) {
		t.Fatalf("want a plain error for a failed read, got %v", err)
	}
}
