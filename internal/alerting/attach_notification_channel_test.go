package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

const masterProperty = `{"webhook_headers":{"Authorization":"` + channelSecret + `"},"webhook_ca_certificate":"CA-` + channelSecret + `","slack_app_mode":true,"service_owner_handle":"@oncall","future_key":1}`

const masterListJSON = `[{"id":7,"name":"hook","type":"generic_webhook","property":` + masterProperty + `},{"id":9,"name":"hook","type":"generic_webhook","service_fqid":"g1","severity":"breach"}]`

// withMasterList answers the channel lookup and delegates attach POSTs.
func withMasterList(post func(*http.Request) (int, string)) func(*http.Request) (int, string) {
	return func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, masterListJSON
		}
		return post(r)
	}
}

func attachBodies(s *channelServer) []string {
	var out []string
	for _, c := range s.calls {
		if c.method == "POST" {
			out = append(out, c.body)
		}
	}
	return out
}

func TestAttachNotificationChannelBothMakesTwoCalls(t *testing.T) {
	n := 20
	s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) {
		n++
		return 200, `{"id":` + string(rune('0'+n-20)) + `,"destination":"` + channelSecret + `"}`
	}))
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: " g1 ", Severity: "BOTH"})
	assertNoSecret(t, text, err)
	if err != nil || isErr || len(s.calls) != 3 ||
		s.calls[0] != (recordedCall{"GET", "/notification_settings/", ""}) ||
		s.calls[1] != (recordedCall{"POST", "/notification_settings/7/attach", `{"entity_id":"g1","severity":"breach","property":` + masterProperty + `}`}) ||
		s.calls[2] != (recordedCall{"POST", "/notification_settings/7/attach", `{"entity_id":"g1","severity":"threat","property":` + masterProperty + `}`}) ||
		text != `{"attachments":[{"attachment_id":1,"alert_group_id":"g1","severity":"breach"},{"attachment_id":2,"alert_group_id":"g1","severity":"threat"}]}` {
		t.Fatalf("text=%s isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestAttachNotificationChannelSingleSeverity(t *testing.T) {
	s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) { return 200, `{"id":5}` }))
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "threat"})
	if err != nil || isErr || len(s.calls) != 2 || !strings.Contains(text, `"severity":"threat"`) {
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
	s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			return 200, `{"id":11}`
		}
		return 400, "bad " + channelSecret
	}))
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "both"})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.HasPrefix(text, "attached breach (attachment_id 11) but threat failed: the API rejected the attach (HTTP 400)") ||
		!strings.HasSuffix(text, "breach stays attached; to finish, retry with severity=threat (re-attaching is idempotent, so severity=both is also safe)") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
	s = newChannelServer(t, withMasterList(func(*http.Request) (int, string) { return 404, channelSecret }))
	text, isErr, err = runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "breach"})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || !strings.Contains(text, "channel not found") {
		t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
	}
}

func TestAttachForwardsMasterPropertyVerbatim(t *testing.T) {
	s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) { return 200, `{"id":11}` }))
	text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g", Severity: "breach"})
	assertNoSecret(t, text, err)
	bodies := attachBodies(s)
	if err != nil || isErr || len(bodies) != 1 {
		t.Fatalf("text=%s isErr=%v err=%v bodies=%v", text, isErr, err, bodies)
	}
	for _, want := range []string{`"webhook_ca_certificate":"CA-` + channelSecret + `"`, `"slack_app_mode":true`, `"service_owner_handle":"@oncall"`, `"future_key":1`} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("attach body missing %s: %s", want, bodies[0])
		}
	}
}

func TestAttachReattachSendsMasterProperty(t *testing.T) {
	s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) { return 200, `{"id":9,"service_fqid":"g1","severity":"breach"}` }))
	for i := 0; i < 2; i++ {
		if text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "g1", Severity: "breach"}); err != nil || isErr {
			t.Fatalf("text=%s isErr=%v err=%v", text, isErr, err)
		}
	}
	bodies := attachBodies(s)
	if len(bodies) != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[1], `"property":`+masterProperty) {
		t.Fatalf("reattach wiped or changed property: %v", bodies)
	}
}

func TestAttachRefusesNonChannelIDs(t *testing.T) {
	for name, id := range map[string]int{"missing": 99, "attachment row": 9} {
		s := newChannelServer(t, withMasterList(func(*http.Request) (int, string) { return 200, `{}` }))
		text, isErr, err := runAttachChannel(s, AttachNotificationChannelArgs{ChannelID: id, AlertGroupID: "g", Severity: "both"})
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, "not a channel id") || s.count("POST") != 0 {
			t.Errorf("%s: text=%s isErr=%v err=%v calls=%+v", name, text, isErr, err, s.calls)
		}
	}
}

func TestAttachPreservesWebhookAuthorization(t *testing.T) {
	const secret = "synthetic-review-header"
	var created, attached struct {
		Property struct {
			WebhookHeaders map[string]string `json:"webhook_headers"`
		} `json:"property"`
	}
	channel := `{"id":7,"name":"secured","type":"generic_webhook","property":{"webhook_headers":{"Authorization":"` + secret + `"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/notification_settings/":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(channel))
		case r.Method == "GET" && r.URL.Path == "/notification_settings/":
			_, _ = w.Write([]byte("[" + channel + "]"))
		case r.Method == "POST" && r.URL.Path == "/notification_settings/7/attach":
			if err := json.NewDecoder(r.Body).Decode(&attached); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"id":11,"name":"secured","type":"generic_webhook","service_fqid":"group-a","severity":"breach"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := testAlertMutationConfig(server.URL)
	createdResult, _, err := NewCreateNotificationChannelHandler(server.Client(), cfg)(context.Background(), nil, CreateNotificationChannelArgs{
		Name: "secured", Type: "generic_webhook", Destination: "https://synthetic.invalid/hook", WebhookHeaders: map[string]string{"Authorization": secret},
	})
	if err != nil || createdResult == nil || createdResult.IsError {
		t.Fatalf("create failed: %v %+v", err, createdResult)
	}
	if created.Property.WebhookHeaders["Authorization"] != secret {
		t.Fatal("positive control: create omitted authorization")
	}
	attachedResult, _, err := NewAttachNotificationChannelHandler(server.Client(), cfg)(context.Background(), nil, AttachNotificationChannelArgs{ChannelID: 7, AlertGroupID: "group-a", Severity: "breach"})
	if err != nil || attachedResult == nil || attachedResult.IsError {
		t.Fatalf("attach failed: %v %+v", err, attachedResult)
	}
	if strings.Contains(resultText(createdResult)+resultText(attachedResult), secret) {
		t.Fatal("secret leaked in tool output")
	}
	if attached.Property.WebhookHeaders["Authorization"] != secret {
		t.Fatal("attach reported success but omitted Authorization from the attachment property")
	}
}
