package traces

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestListSearchTagsRequestAndResponse(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got = r
		_, _ = io.WriteString(w, `{"scopes":[{"name":"span","tags":["http.method"]},{"name":"resource","tags":["service.name"]}]}`)
	}))
	t.Cleanup(srv.Close)
	cfg := models.Config{
		APIBaseURL:   srv.URL,
		Region:       "ap-south-1",
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	}

	resp, err := ListSearchTags(context.Background(), last9api.NewClient(srv.Client(), cfg), ListSearchTagsInput{Start: 100, End: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Scopes) != 2 || resp.Scopes[1].Name != "resource" || !slices.Equal(resp.Scopes[1].Tags, []string{"service.name"}) {
		t.Errorf("scopes = %+v", resp.Scopes)
	}
	q := got.URL.Query()
	if got.Method != http.MethodGet || got.URL.Path != "/cat/api/search/tags" || q.Get("start") != "100" || q.Get("end") != "200" || q.Get("region") != "ap-south-1" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
}
