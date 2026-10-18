package alerting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const deleteRows = `[
 {"id":12,"name":"oncall","type":"slack","service_fqid":"g1","severity":"breach","destination":"` + channelSecret + `"},
 {"id":40,"name":"master","type":"slack","service_fqid":"","destination":"` + channelSecret + `"}]`

// newDeleteServer: a successful DELETE removes the channel from later listings unless stale.
func newDeleteServer(t *testing.T, deleteStatus int, stale bool) *channelServer {
	deleted := false
	return newChannelServer(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodDelete {
			if deleteStatus < 300 && !stale {
				deleted = true
			}
			return deleteStatus, channelSecret
		}
		if deleted {
			return 200, `[{"id":12,"name":"oncall","type":"slack","service_fqid":"g1"}]`
		}
		return 200, deleteRows
	})
}

func runDeleteChannel(s *channelServer, id int) (string, bool, error) {
	res, _, err := NewDeleteNotificationChannelHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, DeleteNotificationChannelArgs{ChannelID: id})
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestDeleteNotificationChannelSuccessVerifiesAfter(t *testing.T) {
	s := newDeleteServer(t, 200, false)
	text, isErr, err := runDeleteChannel(s, 40)
	assertNoSecret(t, text, err)
	if err != nil || isErr || s.count("DELETE") != 1 || len(s.calls) != 3 ||
		s.calls[1] != (recordedCall{"DELETE", "/notification_settings/40/attach", ""}) ||
		s.calls[2].method != "GET" ||
		text != `{"deleted":true,"id":40,"name":"master","type":"slack"}` {
		t.Fatalf("text=%s isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestDeleteNotificationChannelRefusals(t *testing.T) {
	for name, tt := range map[string]struct {
		id   int
		want string
	}{
		"attachment row": {12, "detach_notification_channel"},
		"missing id":     {999, "no channel or attachment with that id"},
		"zero id":        {0, "channel_id is required"},
	} {
		s := newDeleteServer(t, 200, false)
		text, isErr, err := runDeleteChannel(s, tt.id)
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, tt.want) || s.count("DELETE") != 0 {
			t.Errorf("%s: text=%q isErr=%v err=%v calls=%+v", name, text, isErr, err, s.calls)
		}
	}
}

func TestDeleteNotificationChannelNamesAnAttachmentID(t *testing.T) {
	// Like the API: the channel list omits attachment rows; only exact=true returns them.
	s := newChannelServer(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("exact") == "true" {
			return 200, `[{"id":12,"name":"oncall","type":"slack","service_fqid":"g1","severity":"breach","destination":"` + channelSecret + `"}]`
		}
		return 200, `[{"id":40,"name":"master","type":"slack","service_fqid":"","destination":"` + channelSecret + `"}]`
	})
	text, isErr, err := runDeleteChannel(s, 12)
	assertNoSecret(t, text, err)
	if err != nil || !isErr || s.count("DELETE") != 0 {
		t.Fatalf("text=%q isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
	for _, want := range []string{"attachment id", "g1", "detach_notification_channel", "nothing was deleted"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q: %s", want, text)
		}
	}
	if missing, _, _ := runDeleteChannel(s, 999); !strings.Contains(missing, "no channel or attachment with that id") {
		t.Errorf("unknown id: %s", missing)
	}
}

func TestDeleteNotificationChannelStatusMapping(t *testing.T) {
	for status, want := range map[int]string{
		403: "delete-scope",
		422: "detach those attachments first",
		500: "HTTP 500",
	} {
		s := newDeleteServer(t, status, false)
		text, isErr, err := runDeleteChannel(s, 40)
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, want) || s.count("DELETE") != 1 {
			t.Errorf("status %d: text=%q isErr=%v err=%v", status, text, isErr, err)
		}
	}
}

func TestDeleteNotificationChannelStillPresentIsError(t *testing.T) {
	s := newDeleteServer(t, 200, true)
	text, isErr, err := runDeleteChannel(s, 40)
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.Contains(text, "delete was not applied") || strings.Contains(text, `"deleted":true`) {
		t.Fatalf("text=%q isErr=%v err=%v", text, isErr, err)
	}
}
