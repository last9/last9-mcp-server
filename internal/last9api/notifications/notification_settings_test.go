package notifications

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func serve(t *testing.T, wantMethod, wantURI, wantBody, resp string) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if r.Method != wantMethod || r.URL.RequestURI() != wantURI || string(got) != wantBody {
			t.Errorf("request = %s %s %s", r.Method, r.URL.RequestURI(), got)
		}
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
	})
}

func TestListNotificationSettings(t *testing.T) {
	for exact, uri := range map[bool]string{true: "/notification_settings/?exact=true", false: "/notification_settings/"} {
		c := serve(t, http.MethodGet, uri, "", `[{"id":3,"name":"n","type":"slack","destination":"secret","service_fqid":"g"}]`)
		got, err := ListNotificationSettings(context.Background(), c, exact)
		if err != nil || len(got) != 1 || got[0].ID != 3 || got[0].ServiceFQID != "g" {
			t.Fatalf("exact=%v got=%+v err=%v", exact, got, err)
		}
	}
}

func TestCreateNotificationSetting(t *testing.T) {
	yes := true
	c := serve(t, http.MethodPost, "/notification_settings/",
		`{"name":"n","type":"slack","destination":"d","send_resolved":true,"property":{"slack_app_mode":true}}`,
		`{"id":9,"name":"n","type":"slack","send_resolved":true}`)
	got, err := CreateNotificationSetting(context.Background(), c, CreateNotificationSettingInput{
		Name: "n", Type: "slack", Destination: "d", SendResolved: &yes, Property: &Property{SlackAppMode: true},
	})
	if err != nil || got.ID != 9 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestAttachAndDetachNotificationSetting(t *testing.T) {
	c := serve(t, http.MethodPost, "/notification_settings/7/attach", `{"entity_id":"g","severity":"breach"}`, `{"id":12,"service_fqid":"g","severity":"breach"}`)
	got, err := AttachNotificationSetting(context.Background(), c, 7, AttachNotificationSettingInput{EntityID: "g", Severity: "breach"})
	if err != nil || got.ID != 12 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	c = serve(t, http.MethodDelete, "/notification_settings/12/attach", "", "")
	if err := DetachNotificationSetting(context.Background(), c, 12); err != nil {
		t.Fatal(err)
	}
}
