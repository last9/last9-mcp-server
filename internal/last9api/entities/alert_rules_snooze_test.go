package entities

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestSnoozeAlertRulesRequest(t *testing.T) {
	for _, n := range []int64{1900000000, 0, -1} {
		until := strconv.FormatInt(n, 10)
		t.Run(until, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity%2F1/alert-rules/snooze" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != `{"until":`+until+`}` {
					t.Errorf("body = %s", got)
				}
				_, _ = w.Write([]byte(`{"ident":"entity/1"}`))
			}))
			defer srv.Close()
			raw, err := SnoozeAlertRules(context.Background(), newTestClient(srv), SnoozeAlertRulesInput{EntityID: "entity/1", Until: n})
			if err != nil || string(raw) != `{"ident":"entity/1"}` {
				t.Fatalf("raw=%s err=%v", raw, err)
			}
		})
	}
}
