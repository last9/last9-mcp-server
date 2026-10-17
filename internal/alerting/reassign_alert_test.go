package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const reassignClaimJSON = `{"owner_user_id":"u2","owner_name":"B","owner_email":"b@x.io","claimed_by_user_id":"u1","claimed_at":7,"muted_until":0}`

type reassignServer struct {
	*httptest.Server
	body       string
	reassigned bool
	monitor    string
	monitored  bool
}

func newReassignServer(t *testing.T, status int, errBody string) *reassignServer {
	s := &reassignServer{monitor: ackMonitorJSON}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alerts/monitor":
			s.monitored = true
			_, _ = w.Write([]byte(s.monitor))
		case "/users":
			_, _ = w.Write([]byte(`[{"id":"old","email":"b@x.io","deleted_at":1},{"id":"u2","email":"B@X.io"}]`))
		case "/alert-episode-claims/reassign":
			s.reassigned = true
			b, _ := io.ReadAll(r.Body)
			s.body = string(b)
			if status != 0 {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(errBody))
				return
			}
			_, _ = w.Write([]byte(reassignClaimJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func runReassign(s *reassignServer, args ReassignAlertArgs) (string, bool, error) {
	args.AlertGroupID, args.RuleID, args.RuleType = "g", "r", "static"
	res, _, err := NewReassignAlertHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

const wantReassignBody = `{"alert_group_id":"g","rule_id":"r","rule_type":"static","owner_user_id":"u2"}`

func TestReassignAlertByUserID(t *testing.T) {
	s := newReassignServer(t, 0, "")
	text, isErr, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"})
	if err != nil || isErr || text != reassignClaimJSON || s.body != wantReassignBody {
		t.Fatalf("text=%s isErr=%v err=%v body=%s", text, isErr, err, s.body)
	}
}

func TestReassignAlertByEmailSkipsDeletedCaseInsensitive(t *testing.T) {
	s := newReassignServer(t, 0, "")
	text, isErr, err := runReassign(s, ReassignAlertArgs{OwnerEmail: " B@x.IO "})
	if err != nil || isErr || text != reassignClaimJSON || s.body != wantReassignBody {
		t.Fatalf("text=%s isErr=%v err=%v body=%s", text, isErr, err, s.body)
	}
}

func TestReassignAlertValidation(t *testing.T) {
	for name, args := range map[string]ReassignAlertArgs{
		"both":    {OwnerEmail: "b@x.io", OwnerUserID: "u2"},
		"neither": {},
	} {
		t.Run(name, func(t *testing.T) {
			s := newReassignServer(t, 0, "")
			text, isErr, err := runReassign(s, args)
			if err != nil || !isErr || !strings.Contains(text, "exactly one") || s.reassigned {
				t.Fatalf("text=%s isErr=%v err=%v reassigned=%v", text, isErr, err, s.reassigned)
			}
		})
	}
	s := newReassignServer(t, 0, "")
	res, _, err := NewReassignAlertHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, ReassignAlertArgs{OwnerUserID: "u2"})
	if err != nil || !res.IsError {
		t.Fatalf("missing identity not rejected: %+v %v", res, err)
	}
}

func TestReassignAlertEmailNotFound(t *testing.T) {
	s := newReassignServer(t, 0, "")
	text, isErr, err := runReassign(s, ReassignAlertArgs{OwnerEmail: "nobody@x.io"})
	if err != nil || !isErr || text != "no active user with that email" || s.reassigned {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
}

func TestReassignAlertErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not claimed", 404, "", "no owner yet"},
		{"forbidden", 403, "", "org admin or the alert's current owner"},
		{"invalid target", 400, `{"error":"invalid_target_owner"}`, "needs editor or admin role"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newReassignServer(t, tt.status, tt.body)
			text, isErr, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"})
			if err != nil || !isErr || !strings.Contains(text, tt.want) {
				t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
			}
		})
	}
	for _, status := range []int{400, 500} {
		s := newReassignServer(t, status, `{"error":"invalid_episode_identity"}`)
		if _, _, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"}); err == nil {
			t.Errorf("status %d should pass through as error", status)
		}
	}
}

// The reassign API answers 404 both for an unclaimed alert and for ids that match no
// alert, so only the monitor can tell the two apart.
func TestReassignAlertNotFoundNamesUnknownAlert(t *testing.T) {
	s := newReassignServer(t, 404, "")
	s.monitor = `{"alert_rules":[{"alert_group_id":"g","rule_id":"other","rule_type":"static"}]}`
	text, isErr, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"})
	if err != nil || !isErr || !strings.Contains(text, "no alert with this alert_group_id, rule_id and rule_type fired in the last hour") || strings.Contains(text, "no owner yet") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
}

func TestReassignAlertChecksMonitorOnlyAfterNotFound(t *testing.T) {
	s := newReassignServer(t, 0, "")
	if _, isErr, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"}); err != nil || isErr || s.monitored {
		t.Fatalf("isErr=%v err=%v monitored=%v", isErr, err, s.monitored)
	}
}

func TestReassignAlertNotFoundMonitorFailureIsError(t *testing.T) {
	s := newReassignServer(t, 404, "")
	s.monitor = `not json`
	if _, _, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"}); err == nil {
		t.Fatal("a failed monitor lookup must not be reported as a missing owner")
	}
}
