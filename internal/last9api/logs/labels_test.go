package logs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func testClient(t *testing.T, body string, got *url.URL) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		*got = *r.URL
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}
	return last9api.NewClient(srv.Client(), cfg)
}

func TestListLabelsRequestAndResponse(t *testing.T) {
	var got url.URL
	c := testClient(t, `{"status":"success","data":["service","level"]}`, &got)
	labels, err := ListLabels(context.Background(), c, ListLabelsInput{Start: 100, End: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(labels, []string{"service", "level"}) {
		t.Errorf("labels = %v", labels)
	}
	q := got.Query()
	if got.Path != "/logs/api/v1/labels" || q.Get("start") != "100" || q.Get("end") != "200" || q.Get("region") != "ap-south-1" {
		t.Errorf("request = %s", got.String())
	}
}

func TestListLabelsRejectsNonSuccessStatus(t *testing.T) {
	var got url.URL
	c := testClient(t, `{"status":"error","data":["x"]}`, &got)
	labels, err := ListLabels(context.Background(), c, ListLabelsInput{Start: 1, End: 2})
	if err == nil || !strings.Contains(err.Error(), `status "error"`) {
		t.Fatalf("err = %v, want status error", err)
	}
	if labels != nil {
		t.Errorf("labels = %v, want none alongside the error", labels)
	}
}
