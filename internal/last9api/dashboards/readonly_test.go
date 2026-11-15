package dashboards

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"last9-mcp/internal/utils"
)

func TestSetDashboardReadonly(t *testing.T) {
	for _, readonly := range []bool{true, false} {
		var gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch || r.URL.EscapedPath() != "/dashboards/a%2Fb/readonly" || r.URL.RawQuery != "" {
				t.Errorf("request = %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery)
			}
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			_, _ = io.WriteString(w, gotBody[:len(gotBody)-1]+`,"id":"a/b"}`)
		}))

		got, err := SetDashboardReadonly(context.Background(), newClient(srv.URL, srv.Client()), "a/b", readonly)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := `{"readonly":false}`
		if readonly {
			want = `{"readonly":true}`
		}
		if gotBody != want {
			t.Errorf("body = %s, want %s", gotBody, want)
		}
		if got.ID != "a/b" || got.Readonly == nil || *got.Readonly != readonly {
			t.Errorf("got %+v", got)
		}
	}
}

func TestSetDashboardReadonlyErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"forbidden keeps body", http.StatusForbidden, `{"message":"built-in dashboards are read-only"}`, func(err error) bool {
			var u *utils.UpstreamHTTPError
			return errors.As(err, &u) && u.StatusCode == http.StatusForbidden && u.Body == `{"message":"built-in dashboards are read-only"}`
		}},
		{"not found", http.StatusNotFound, `{"message":"x"}`, func(err error) bool {
			var u *utils.UpstreamHTTPError
			return errors.As(err, &u) && u.StatusCode == http.StatusNotFound
		}},
		{"empty 2xx", http.StatusOK, ``, func(err error) bool { return err != nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			_, err := SetDashboardReadonly(context.Background(), newClient(srv.URL, srv.Client()), "d", true)
			if !tt.check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
