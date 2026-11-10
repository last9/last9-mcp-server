package alerts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReassignAlertEpisode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/alert-episode-claims/reassign" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		want := `{"alert_group_id":"g","rule_id":"r","rule_type":"static","owner_user_id":"u"}`
		if got, _ := io.ReadAll(r.Body); string(got) != want {
			t.Errorf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"owner_user_id":"u","owner_name":"A","owner_email":"a@x.io","claimed_by_user_id":"c","claimed_at":5,"muted_until":0}`))
	}))
	defer srv.Close()
	got, err := ReassignAlertEpisode(context.Background(), newTestClient(srv),
		ReassignAlertEpisodeInput{AlertGroupID: "g", RuleID: "r", RuleType: "static", OwnerUserID: "u"})
	if err != nil || got.OwnerEmail != "a@x.io" || got.ClaimedAt != 5 || got.ClaimedByUserID != "c" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestClaimAlertEpisode(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   ClaimAlertEpisodeInput
		want string
	}{
		{"minimal", ClaimAlertEpisodeInput{AlertGroupID: "g", RuleID: "r", RuleType: "static"},
			`{"alert_group_id":"g","rule_id":"r","rule_type":"static"}`},
		{"with since and mute", ClaimAlertEpisodeInput{AlertGroupID: "g", RuleID: "r", RuleType: "static", Since: 10, MuteUntil: 99},
			`{"alert_group_id":"g","rule_id":"r","rule_type":"static","since":10,"mute_until":99}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/alert-episode-claims/claim" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != tt.want {
					t.Errorf("body = %s", got)
				}
				_, _ = w.Write([]byte(`{"owner_user_id":"u","claimed_by_user_id":"u","claimed_at":5,"muted_until":99}`))
			}))
			defer srv.Close()
			got, err := ClaimAlertEpisode(context.Background(), newTestClient(srv), tt.in)
			if err != nil || got.OwnerUserID != "u" || got.ClaimedAt != 5 || got.MutedUntil != 99 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestUnackAlertEpisode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/alert-episode-claims/unack" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		want := `{"alert_group_id":"g","rule_id":"r","rule_type":"static"}`
		if got, _ := io.ReadAll(r.Body); string(got) != want {
			t.Errorf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"released":true}`))
	}))
	defer srv.Close()
	if err := UnackAlertEpisode(context.Background(), newTestClient(srv),
		UnackAlertEpisodeInput{AlertGroupID: "g", RuleID: "r", RuleType: "static"}); err != nil {
		t.Fatal(err)
	}
}
