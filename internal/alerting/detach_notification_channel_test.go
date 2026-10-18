package alerting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const detachRows = `[
 {"id":12,"name":"oncall","type":"slack","service_fqid":"g1","severity":"breach","destination":"` + channelSecret + `"},
 {"id":40,"name":"master","type":"slack","service_fqid":""}]`

func newDetachServer(t *testing.T, deleteStatus int) *channelServer {
	return newChannelServer(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodDelete {
			return deleteStatus, channelSecret
		}
		return 200, detachRows
	})
}

func runDetachChannel(s *channelServer, args DetachNotificationChannelArgs) (string, bool, error) {
	res, _, err := NewDetachNotificationChannelHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestDetachNotificationChannelRefusals(t *testing.T) {
	for name, tt := range map[string]struct {
		args DetachNotificationChannelArgs
		want string
	}{
		"master channel id (empty fqid)": {DetachNotificationChannelArgs{AttachmentID: 40}, "would delete the channel"},
		"unknown id":                     {DetachNotificationChannelArgs{AttachmentID: 999}, "would delete the channel"},
		"group mismatch":                 {DetachNotificationChannelArgs{AttachmentID: 12, AlertGroupID: "other"}, "different alert group"},
		"missing id":                     {DetachNotificationChannelArgs{}, "attachment_id is required"},
	} {
		s := newDetachServer(t, 200)
		text, isErr, err := runDetachChannel(s, tt.args)
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, tt.want) || s.count("DELETE") != 0 {
			t.Errorf("%s: text=%q isErr=%v err=%v calls=%+v", name, text, isErr, err, s.calls)
		}
	}
}

func TestDetachNotificationChannelAllowsAttachmentRow(t *testing.T) {
	for _, group := range []string{"", " g1 "} {
		s := newDetachServer(t, 200)
		text, isErr, err := runDetachChannel(s, DetachNotificationChannelArgs{AttachmentID: 12, AlertGroupID: group})
		assertNoSecret(t, text, err)
		if err != nil || isErr || s.count("DELETE") != 1 ||
			s.calls[0] != (recordedCall{"GET", "/notification_settings/?exact=true", ""}) ||
			s.calls[1] != (recordedCall{"DELETE", "/notification_settings/12/attach", ""}) ||
			text != `{"alert_group_id":"g1","attachment_id":12,"channel_name":"oncall","detached":true,"severity":"breach"}` {
			t.Fatalf("group=%q text=%s isErr=%v err=%v calls=%+v", group, text, isErr, err, s.calls)
		}
	}
}

func TestDetachNotificationChannelForbiddenNamesDeleteScope(t *testing.T) {
	s := newDetachServer(t, 403)
	text, isErr, err := runDetachChannel(s, DetachNotificationChannelArgs{AttachmentID: 12})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.Contains(text, "delete-scope") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
}
