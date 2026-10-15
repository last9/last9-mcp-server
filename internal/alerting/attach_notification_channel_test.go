package alerting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func runAttachChannel(s *channelServer, args AttachNotificationChannelArgs) (string, bool, error) {
	res, _, err := NewAttachNotificationChannelHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestAttachNotificationChannelBothMakesTwoCalls(t *testing.T) {
	n := 20
	s := newChannelServer(t, func(*http.Request) (int, string) {
		n++
		return 200, `{"id":` + string(rune('0'+n-20)) + `,"destination":"` + channelSecret + `"}`
	})
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: " g1 ", Severity: "BOTH"})
	assertNoSecret(t, text, err)
	if err != nil || isErr || len(s.calls) != 2 ||
		s.calls[0] != (recordedCall{"POST", "/notification_settings/7/attach", `{"entity_id":"g1","severity":"breach"}`}) ||
		s.calls[1] != (recordedCall{"POST", "/notification_settings/7/attach", `{"entity_id":"g1","severity":"threat"}`}) ||
		text != `{"attachments":[{"attachment_id":1,"alert_group_id":"g1","severity":"breach"},{"attachment_id":2,"alert_group_id":"g1","severity":"threat"}]}` {
		t.Fatalf("text=%s isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestAttachNotificationChannelSingleSeverity(t *testing.T) {
	s := newChannelServer(t, func(*http.Request) (int, string) { return 200, `{"id":5}` })
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "threat"})
	if err != nil || isErr || len(s.calls) != 1 || !strings.Contains(text, `"severity":"threat"`) {
		t.Fatalf("text=%s isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestAttachNotificationChannelValidation(t *testing.T) {
	for name, args := range map[string]AttachNotificationChannelArgs{
		"bad severity": {ChannelID: 1, AlertGroupID: "g", Severity: "critical"},
		"no severity":  {ChannelID: 1, AlertGroupID: "g"},
		"no channel":   {AlertGroupID: "g", Severity: "breach"},
		"no group":     {ChannelID: 1, AlertGroupID: " ", Severity: "breach"},
	} {
		s := newChannelServer(t, func(*http.Request) (int, string) { return 200, `{}` })
		text, isErr, err := runAttachChannel(s, args)
		if err != nil || !isErr || len(s.calls) != 0 {
			t.Errorf("%s: text=%s isErr=%v err=%v calls=%d", name, text, isErr, err, len(s.calls))
		}
	}
}

func TestAttachNotificationChannelPartialFailureAndSafeErrors(t *testing.T) {
	calls := 0
	s := newChannelServer(t, func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			return 200, `{"id":11}`
		}
		return 400, "bad " + channelSecret
	})
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "both"})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.Contains(text, "attached breach (attachment_id 11) but threat failed") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
	s = newChannelServer(t, func(*http.Request) (int, string) { return 404, channelSecret })
	text, isErr, err = runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "breach"})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.Contains(text, "channel not found") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
}
