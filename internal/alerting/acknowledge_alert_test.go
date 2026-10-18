package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const ackClaimJSON = `{"owner_user_id":"u1","owner_name":"A","owner_email":"a@x.io","claimed_by_user_id":"u1","claimed_at":7,"muted_until":0}`

// ackMonitorJSON lists the alert runAck targets by default, so the existence check passes.
const ackMonitorJSON = `{"alert_rules":[{"alert_group_id":"g","rule_id":"r","rule_type":"static"}]}`

type ackServer struct {
	*httptest.Server
	path, body   string
	monitor      string
	monitorQuery url.Values
}

func newAckServer(t *testing.T, status int, respBody string) *ackServer {
	s := &ackServer{monitor: ackMonitorJSON}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodGet && r.URL.Path == "/alerts/monitor" {
			s.monitorQuery = r.URL.Query()
			_, _ = w.Write([]byte(s.monitor))
			return
		}
		s.path, s.body = r.URL.Path, string(b)
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(respBody))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/unack") {
			_, _ = w.Write([]byte(`{"released":true}`))
			return
		}
		_, _ = w.Write([]byte(ackClaimJSON))
	}))
	t.Cleanup(s.Close)
	return s
}

func runAck(s *ackServer, args AcknowledgeAlertArgs) (string, bool, error) {
	if args.AlertGroupID == "" {
		args.AlertGroupID, args.RuleID, args.RuleType = "g", "r", "static"
	}
	res, _, err := NewAcknowledgeAlertHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestAcknowledgeAlertDefaultsToAck(t *testing.T) {
	s := newAckServer(t, 0, "")
	text, isErr, err := runAck(s, AcknowledgeAlertArgs{})
	if err != nil || isErr || text != ackClaimJSON || s.path != "/alert-episode-claims/claim" ||
		s.body != `{"alert_group_id":"g","rule_id":"r","rule_type":"static"}` {
		t.Fatalf("text=%s isErr=%v err=%v path=%s body=%s", text, isErr, err, s.path, s.body)
	}
}

func TestAcknowledgeAlertWithMuteUntil(t *testing.T) {
	s := newAckServer(t, 0, "")
	_, isErr, err := runAck(s, AcknowledgeAlertArgs{Action: " ACK ", MuteUntil: 1700000000})
	if err != nil || isErr || s.body != `{"alert_group_id":"g","rule_id":"r","rule_type":"static","mute_until":1700000000}` {
		t.Fatalf("isErr=%v err=%v body=%s", isErr, err, s.body)
	}
}

func TestAcknowledgeAlertRefusesAlertNotInMonitor(t *testing.T) {
	for name, args := range map[string]AcknowledgeAlertArgs{
		"unknown rule":  {AlertGroupID: "g", RuleID: "00000000-0000-0000-0000-000000000000", RuleType: "static"},
		"unknown group": {AlertGroupID: "00000000-0000-0000-0000-000000000000", RuleID: "r", RuleType: "static"},
		"other type":    {AlertGroupID: "g", RuleID: "r", RuleType: "changepoint"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newAckServer(t, 0, "")
			text, isErr, err := runAck(s, args)
			if err != nil || !isErr || !strings.Contains(text, "nothing was changed") || s.path != "" {
				t.Fatalf("text=%s isErr=%v err=%v claim path=%s", text, isErr, err, s.path)
			}
		})
	}
	s := newAckServer(t, 0, "")
	s.monitor = `{"alert_rules":[]}`
	if text, isErr, err := runAck(s, AcknowledgeAlertArgs{}); err != nil || !isErr || s.path != "" {
		t.Fatalf("empty monitor: text=%s isErr=%v err=%v claim path=%s", text, isErr, err, s.path)
	}
}

func TestAcknowledgeAlertMatchesMonitorCaseInsensitively(t *testing.T) {
	s := newAckServer(t, 0, "")
	s.monitor = `{"alert_rules":[{"alert_group_id":"G","rule_id":"R","rule_type":"Static"}]}`
	if text, isErr, err := runAck(s, AcknowledgeAlertArgs{}); err != nil || isErr || s.path != "/alert-episode-claims/claim" {
		t.Fatalf("text=%s isErr=%v err=%v claim path=%s", text, isErr, err, s.path)
	}
	if s.monitorQuery.Get("window") != "3600" || s.monitorQuery.Get("timestamp") == "" {
		t.Fatalf("monitor query = %v", s.monitorQuery)
	}
}

func TestAcknowledgeAlertUnackSkipsMonitor(t *testing.T) {
	s := newAckServer(t, 0, "")
	s.monitor = `{"alert_rules":[]}`
	if text, isErr, err := runAck(s, AcknowledgeAlertArgs{Action: "unack"}); err != nil || isErr || s.path != "/alert-episode-claims/unack" {
		t.Fatalf("text=%s isErr=%v err=%v path=%s", text, isErr, err, s.path)
	}
}

func TestAcknowledgeAlertUnack(t *testing.T) {
	s := newAckServer(t, 0, "")
	text, isErr, err := runAck(s, AcknowledgeAlertArgs{Action: "unack"})
	if err != nil || isErr || text != `{"released":true}` || s.path != "/alert-episode-claims/unack" ||
		s.body != `{"alert_group_id":"g","rule_id":"r","rule_type":"static"}` {
		t.Fatalf("text=%s isErr=%v err=%v path=%s body=%s", text, isErr, err, s.path, s.body)
	}
}

func TestAcknowledgeAlertValidation(t *testing.T) {
	for name, tt := range map[string]struct {
		args AcknowledgeAlertArgs
		want string
	}{
		"missing ids":     {AcknowledgeAlertArgs{AlertGroupID: " ", RuleID: "r", RuleType: "static"}, "are required"},
		"bad action":      {AcknowledgeAlertArgs{Action: "claim"}, "ack or unack"},
		"negative mute":   {AcknowledgeAlertArgs{MuteUntil: -1}, "mute_until must be"},
		"mute with unack": {AcknowledgeAlertArgs{Action: "unack", MuteUntil: 5}, "only to action ack"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newAckServer(t, 0, "")
			args := tt.args
			if name == "missing ids" {
				res, _, err := NewAcknowledgeAlertHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
				if err != nil || !res.IsError || !strings.Contains(resultText(res), tt.want) || s.path != "" {
					t.Fatalf("res=%+v err=%v path=%s", res, err, s.path)
				}
				return
			}
			text, isErr, err := runAck(s, args)
			if err != nil || !isErr || !strings.Contains(text, tt.want) || s.path != "" {
				t.Fatalf("text=%s isErr=%v err=%v path=%s", text, isErr, err, s.path)
			}
		})
	}
}

func TestAcknowledgeAlertErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		name, action string
		status       int
		want         string
	}{
		{"owned by other", "ack", 409, "already acknowledged by another user"},
		{"ack forbidden", "ack", 403, "editor or admin role"},
		{"unack forbidden", "unack", 403, "org admin or the alert's current owner"},
		{"unack not found", "unack", 404, "no acknowledgement to release"},
		{"ack not found", "ack", 404, "re-run get_alerts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newAckServer(t, tt.status, "")
			text, isErr, err := runAck(s, AcknowledgeAlertArgs{Action: tt.action})
			if err != nil || !isErr || !strings.Contains(text, tt.want) {
				t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
			}
		})
	}
	s := newAckServer(t, 400, `{"error":"invalid_episode_identity"}`)
	if _, _, err := runAck(s, AcknowledgeAlertArgs{}); err == nil || !strings.Contains(err.Error(), "invalid_episode_identity") {
		t.Fatalf("400 should pass through with body: %v", err)
	}
	s = newAckServer(t, 409, "claim changed")
	if _, _, err := runAck(s, AcknowledgeAlertArgs{Action: "unack"}); err == nil || !strings.Contains(err.Error(), "claim changed") {
		t.Fatalf("unack 409 should relay body: %v", err)
	}
}
