package alerting

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testCACertPEM(t *testing.T) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func runCreateChannel(s *channelServer, args CreateNotificationChannelArgs) (string, bool, error) {
	res, _, err := NewCreateNotificationChannelHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, args)
	if res == nil {
		return "", false, err
	}
	return resultText(res), res.IsError, err
}

func TestValidateCreateNotificationChannel(t *testing.T) {
	cert := testCACertPEM(t)
	hook := "https://hooks.slack.com/services/" + channelSecret
	for name, tt := range map[string]struct {
		args CreateNotificationChannelArgs
		want string
	}{
		"ok slack webhook":      {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook}, ""},
		"ok slack app":          {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: "C123", SlackAppMode: true}, ""},
		"ok pagerduty":          {CreateNotificationChannelArgs{Name: "n", Type: "pagerduty", Destination: channelSecret}, ""},
		"ok opsgenie":           {CreateNotificationChannelArgs{Name: "n", Type: "opsgenie", Destination: channelSecret}, ""},
		"ok email":              {CreateNotificationChannelArgs{Name: "n", Type: "email", Destination: "a@x.io, b@x.io"}, ""},
		"ok webhook with cert":  {CreateNotificationChannelArgs{Name: "n", Type: "generic_webhook", Destination: "https://h.example", WebhookCACertificate: cert, WebhookHeaders: map[string]string{"Authorization": channelSecret}}, ""},
		"webhook plain http":    {CreateNotificationChannelArgs{Name: "n", Type: "generic_webhook", Destination: "http://example.invalid/hook"}, "HTTPS"},
		"ok mentions":           {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, MentionHandles: "alice, user|U123|bob,usergroup|S1|g,special|here"}, ""},
		"empty name":            {CreateNotificationChannelArgs{Name: "", Type: "slack", Destination: hook}, "name is required"},
		"colon in name":         {CreateNotificationChannelArgs{Name: "a:b", Type: "slack", Destination: hook}, "name is required"},
		"bad type":              {CreateNotificationChannelArgs{Name: "n", Type: "sms", Destination: "1"}, "type must be"},
		"empty destination":     {CreateNotificationChannelArgs{Name: "n", Type: "slack"}, "destination is required"},
		"slack non-hook url":    {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: "https://evil.example/" + channelSecret}, "hooks.slack.com"},
		"slack app with url":    {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, SlackAppMode: true}, "channel ID"},
		"app mode on non-slack": {CreateNotificationChannelArgs{Name: "n", Type: "email", Destination: "a@x.io", SlackAppMode: true}, "only to type slack"},
		"email bad address":     {CreateNotificationChannelArgs{Name: "n", Type: "email", Destination: "a@x.io,nope"}, "email addresses"},
		"headers on slack":      {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, WebhookHeaders: map[string]string{"a": "b"}}, "only to type generic_webhook"},
		"cert on slack":         {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, WebhookCACertificate: cert}, "only to type generic_webhook"},
		"cert garbage":          {CreateNotificationChannelArgs{Name: "n", Type: "generic_webhook", Destination: "https://h", WebhookCACertificate: "nope"}, "PEM CERTIFICATE"},
		"cert bundle":           {CreateNotificationChannelArgs{Name: "n", Type: "generic_webhook", Destination: "https://h", WebhookCACertificate: cert + cert}, "exactly one"},
		"cert too large":        {CreateNotificationChannelArgs{Name: "n", Type: "generic_webhook", Destination: "https://h", WebhookCACertificate: "-----BEGIN CERTIFICATE-----" + strings.Repeat("A", 65*1024)}, "64 KiB"},
		"bad user mention":      {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, MentionHandles: "user|bad|x"}, "mention_handles"},
		"bad special mention":   {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, MentionHandles: "special|everyone"}, "mention_handles"},
		"unknown typed mention": {CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: hook, MentionHandles: "alice, usr|U123|bob"}, "mention_handles"},
	} {
		t.Run(name, func(t *testing.T) {
			got := validateCreateNotificationChannel(tt.args)
			if (tt.want == "" && got != "") || !strings.Contains(got, tt.want) {
				t.Fatalf("got %q want contains %q", got, tt.want)
			}
			if strings.Contains(got, channelSecret) {
				t.Fatalf("validation message leaks secret: %q", got)
			}
		})
	}
}

func TestCreateNotificationChannelSendsBodyAndHidesSecrets(t *testing.T) {
	s := newChannelServer(t, func(*http.Request) (int, string) {
		return 200, `{"id":9,"name":"oncall","type":"slack","send_resolved":true,"destination":"` + channelSecret + `","property":{"webhook_headers":{"a":"` + channelSecret + `"}}}`
	})
	no := false
	text, isErr, err := runCreateChannel(s, CreateNotificationChannelArgs{
		Name: " oncall ", Type: "Slack", Destination: "https://hooks.slack.com/services/" + channelSecret, SendResolved: &no, MentionHandles: "special|here",
	})
	assertNoSecret(t, text, err)
	want := `{"name":"oncall","type":"slack","destination":"https://hooks.slack.com/services/` + channelSecret + `","send_resolved":false,"property":{"service_owner_handle":"special|here"}}`
	if err != nil || isErr || text != `{"id":9,"name":"oncall","type":"slack","send_resolved":true}` ||
		len(s.calls) != 1 || s.calls[0].method != "POST" || s.calls[0].uri != "/notification_settings/" || s.calls[0].body != want {
		t.Fatalf("text=%s isErr=%v err=%v calls=%+v", text, isErr, err, s.calls)
	}
}

func TestCreateNotificationChannelValidationSendsNothing(t *testing.T) {
	s := newChannelServer(t, func(*http.Request) (int, string) { return 200, `{}` })
	text, isErr, err := runCreateChannel(s, CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: "https://x/" + channelSecret})
	assertNoSecret(t, text, err)
	if err != nil || !isErr || len(s.calls) != 0 {
		t.Fatalf("text=%s isErr=%v err=%v calls=%d", text, isErr, err, len(s.calls))
	}
}

// The API's email-destination rejections are fixed strings that echo no input, so the tool
// can name the cause without relaying the body.
func TestCreateNotificationChannelNamesEmailRejection(t *testing.T) {
	for upstream, want := range map[string]string{
		"cannot use personal email for channel destination":  "personal-provider",
		"cannot use non-admin email for channel destination": "only the org admin's address",
		"invalid email":                        "is not a valid email address",
		"no admin user found for organization": "HTTP 400",
		"cannot use personal email for channel destination " + channelSecret: "HTTP 400",
	} {
		s := newChannelServer(t, func(*http.Request) (int, string) {
			return 400, `{"error":"` + upstream + `"}`
		})
		text, isErr, err := runCreateChannel(s, CreateNotificationChannelArgs{Name: "n", Type: "email", Destination: "a@gmail.com"})
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, want) || strings.Contains(text, "a@gmail.com") {
			t.Errorf("%q: text=%q isErr=%v err=%v", upstream, text, isErr, err)
		}
	}
}

func TestCreateNotificationChannelErrorsNeverLeakSecrets(t *testing.T) {
	for status, want := range map[int]string{
		400: "HTTP 400", 422: "HTTP 422", 403: "write-scope", 500: "HTTP 500", 502: "HTTP 502",
		409: "get_notification_channels scope=channels",
	} {
		s := newChannelServer(t, func(*http.Request) (int, string) {
			return status, `invalid destination ` + channelSecret + ` header ` + channelSecret
		})
		text, isErr, err := runCreateChannel(s, CreateNotificationChannelArgs{
			Name: "n", Type: "generic_webhook", Destination: "https://h/" + channelSecret, WebhookHeaders: map[string]string{"k": channelSecret},
		})
		assertNoSecret(t, text, err)
		if err != nil || !isErr || !strings.Contains(text, want) {
			t.Errorf("status %d: text=%q isErr=%v err=%v", status, text, isErr, err)
		}
	}
}
