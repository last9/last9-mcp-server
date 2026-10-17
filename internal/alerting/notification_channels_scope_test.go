package alerting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func runGetChannels(s *channelServer, scope string) (string, bool, error) {
	res, _, err := NewGetNotificationChannelsHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, GetNotificationChannelsArgs{Scope: scope})
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestGetNotificationChannelsScopeChannels(t *testing.T) {
	s := newChannelServer(t, func(*http.Request) (int, string) {
		return 200, `[{"id":4,"name":"oncall","type":"slack","send_resolved":true,"destination":"` + channelSecret + `"},
{"id":5,"name":"mail","type":"email","send_resolved":null,"created_via":"last9-mcp"},
{"id":6,"name":"attached","type":"slack","service_fqid":"g1","severity":"breach"}]`
	})
	text, isErr, err := runGetChannels(s, " Channels ")
	assertNoSecret(t, text, err)
	want := "Found 2 notification channel(s):\nid\tname\ttype\tsend_resolved\tcreated_via\n4\toncall\tslack\ttrue\t-\n5\tmail\temail\tnull\tlast9-mcp"
	if err != nil || isErr || text != want || len(s.calls) != 1 || s.calls[0].uri != "/notification_settings/" {
		t.Fatalf("text=%q isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestGetNotificationChannelsDefaultScopeKeepsExactQuery(t *testing.T) {
	for _, scope := range []string{"", "attached"} {
		s := newChannelServer(t, func(*http.Request) (int, string) { return 200, `[]` })
		text, isErr, err := runGetChannels(s, scope)
		if err != nil || isErr || !strings.HasPrefix(text, "Found 0 notification channel(s):\nid\tname\ttype\tglobal") ||
			len(s.calls) != 1 || s.calls[0].uri != "/notification_settings?exact=true" {
			t.Fatalf("scope=%q text=%q err=%v calls=%+v", scope, text, err, s.calls)
		}
	}
}

func TestGetNotificationChannelsInvalidScope(t *testing.T) {
	s := newChannelServer(t, func(*http.Request) (int, string) { return 200, `[]` })
	text, isErr, err := runGetChannels(s, "all")
	if err != nil || !isErr || !strings.Contains(text, "scope must be") || len(s.calls) != 0 {
		t.Fatalf("text=%q isErr=%v err=%v", text, isErr, err)
	}
}
