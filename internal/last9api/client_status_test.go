package last9api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"last9-mcp/internal/utils"
)

func TestDoExpectedSuccessStatus(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status, expected int
		fail             bool
	}{
		{"default accepts created", 201, 0, false},
		{"default accepts no content", 204, 0, false},
		{"explicit OK", 200, 200, false},
		{"reject created", 201, 200, true},
		{"reject no content", 204, 200, true},
		{"cannot allow server error", 500, 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
			defer srv.Close()
			var opts []Option
			if tc.expected != 0 {
				opts = append(opts, WithSuccessStatus(tc.expected))
			}
			err := NewClient(srv.Client(), testCfg(srv.URL)).Do(context.Background(), "test", Request{Method: http.MethodGet, Path: "/test"}, nil, opts...)
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v, want failure %v", err, tc.fail)
			}
			if tc.fail {
				var upstream *utils.UpstreamHTTPError
				if !errors.As(err, &upstream) || upstream.StatusCode != tc.status {
					t.Fatalf("want typed HTTP %d error, got %v", tc.status, err)
				}
			}
		})
	}
}
