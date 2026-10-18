package users

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestListUsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/users" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`[{"id":"u1","name":"A","email":"a@x.io","role":"admin","status":"active"},{"id":"u2","email":"b@x.io","deleted_at":1700000000}]`))
	}))
	defer srv.Close()
	c := last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)},
	})
	got, err := ListUsers(context.Background(), c)
	if err != nil || len(got) != 2 || got[0].ID != "u1" || got[0].DeletedAt != nil || got[1].DeletedAt == nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
