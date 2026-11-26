package alerting

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/notifications"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxWebhookCACertificateBytes = 64 * 1024

var (
	slackUserIDPattern      = regexp.MustCompile(`^[UW][A-Z0-9]+$`)
	slackUsergroupIDPattern = regexp.MustCompile(`^S[A-Z0-9]+$`)
)

type CreateNotificationChannelArgs struct {
	Name                 string            `json:"name" jsonschema:"(Required) Channel name; must be unique per type and must not contain ':'"`
	Type                 string            `json:"type" jsonschema:"(Required) One of slack, pagerduty, opsgenie, email, generic_webhook"`
	Destination          string            `json:"destination" jsonschema:"(Required) Where notifications go; format depends on type: slack Incoming Webhook URL (https://hooks.slack.com/...) or, with slack_app_mode, a Slack channel ID; pagerduty routing key; opsgenie API key; email comma-separated addresses; generic_webhook the HTTPS URL"`
	SlackAppMode         bool              `json:"slack_app_mode,omitempty" jsonschema:"slack only. true sends through the Last9 Slack app and destination is a channel ID rather than a webhook URL. Default false"`
	MentionHandles       string            `json:"mention_handles,omitempty" jsonschema:"Comma-separated handles to mention or notify, e.g. user|U123ABC|name, usergroup|S123ABC|name, special|here, special|channel. Default none"`
	WebhookHeaders       map[string]string `json:"webhook_headers,omitempty" jsonschema:"generic_webhook only. Extra HTTP headers sent with each call, e.g. an Authorization header. Default none"`
	WebhookCACertificate string            `json:"webhook_ca_certificate,omitempty" jsonschema:"generic_webhook only. One PEM CA certificate (max 64 KiB) to trust for an HTTPS webhook with a private CA. Default none"`
	SendResolved         *bool             `json:"send_resolved,omitempty" jsonschema:"Also notify when the alert resolves. Default true"`
}

type createdNotificationChannel struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	SendResolved *bool  `json:"send_resolved"`
}

func NewCreateNotificationChannelHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, CreateNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args CreateNotificationChannelArgs) (*mcp.CallToolResult, any, error) {
		args.Name, args.Type, args.Destination = strings.TrimSpace(args.Name), strings.ToLower(strings.TrimSpace(args.Type)), strings.TrimSpace(args.Destination)
		if msg := validateCreateNotificationChannel(args); msg != "" {
			return utils.ToolErrorResult(msg), nil, nil
		}
		in := notifications.CreateNotificationSettingInput{
			Name: args.Name, Type: args.Type, Destination: args.Destination, SendResolved: args.SendResolved,
			CreatedVia: newCreatedViaFields(req),
		}
		if prop := (notifications.Property{
			ServiceOwnerHandle:   strings.TrimSpace(args.MentionHandles),
			WebhookHeaders:       args.WebhookHeaders,
			WebhookCACertificate: strings.TrimSpace(args.WebhookCACertificate),
			SlackAppMode:         args.SlackAppMode,
		}); prop.ServiceOwnerHandle != "" || len(prop.WebhookHeaders) > 0 || prop.WebhookCACertificate != "" || prop.SlackAppMode {
			in.Property = &prop
		}
		created, err := notifications.CreateNotificationSetting(ctx, last9api.NewClient(client, cfg), in)
		if err != nil {
			if msg := emailDestinationRejection(err); msg != "" {
				return utils.ToolErrorResult(msg), nil, nil
			}
			return notificationChannelErrorResult(err, map[int]string{
				http.StatusBadRequest:          "the API rejected the channel (HTTP 400); re-check name, type, destination and the optional fields against the tool description",
				http.StatusUnprocessableEntity: "the API rejected the channel (HTTP 422); re-check name, type, destination and the optional fields against the tool description",
				http.StatusConflict:            "a channel with the same name and type, or the same destination, already exists; list channels with get_notification_channels scope=channels and reuse its id",
				http.StatusForbidden:           "creating a channel requires a write-scope token whose owner has the editor or admin role",
			})
		}
		return notificationChannelJSONResult(cfg, createdNotificationChannel{
			ID: created.ID, Name: created.Name, Type: created.Type, SendResolved: created.SendResolved,
		})
	}
}

func validateCreateNotificationChannel(args CreateNotificationChannelArgs) string {
	if args.Name == "" || strings.Contains(args.Name, ":") {
		return "name is required and must not contain ':'"
	}
	switch args.Type {
	case "slack", "pagerduty", "opsgenie", "email", "generic_webhook":
	default:
		return "type must be one of slack, pagerduty, opsgenie, email, generic_webhook"
	}
	if args.Destination == "" {
		return "destination is required"
	}
	if args.Type != "slack" && args.SlackAppMode {
		return "slack_app_mode applies only to type slack"
	}
	if args.Type != "generic_webhook" && (len(args.WebhookHeaders) > 0 || strings.TrimSpace(args.WebhookCACertificate) != "") {
		return "webhook_headers and webhook_ca_certificate apply only to type generic_webhook"
	}
	if msg := validateDestinationForType(args); msg != "" {
		return msg
	}
	if msg := validateWebhookCACertificate(strings.TrimSpace(args.WebhookCACertificate)); msg != "" {
		return msg
	}
	if !validMentionHandles(args.MentionHandles) {
		return "mention_handles contains a malformed typed mention; use user|<U or W id>|name, usergroup|<S id>|name, special|here or special|channel"
	}
	return ""
}

// emailDestinationRejections maps the API's email-destination 400s, fixed strings that echo
// no input, to the cause. Only exact matches are used, so no other body text is relayed.
var emailDestinationRejections = map[string]string{
	"cannot use personal email for channel destination":  "the API rejects personal-provider email addresses (for example Gmail) as a channel destination; use work addresses. Nothing was created",
	"cannot use non-admin email for channel destination": "this org is on a personal login, so the API accepts only the org admin's address as an email destination. Nothing was created",
	"invalid email": "an address in destination is not a valid email address according to the API; nothing was created",
}

func emailDestinationRejection(err error) string {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadRequest {
		return ""
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(upstream.Body), &body) != nil {
		return ""
	}
	return emailDestinationRejections[body.Error]
}

func validateDestinationForType(args CreateNotificationChannelArgs) string {
	switch args.Type {
	case "slack":
		if args.SlackAppMode && strings.HasPrefix(args.Destination, "http") {
			return "with slack_app_mode, destination must be a Slack channel ID, not a webhook URL"
		}
		if !args.SlackAppMode && !strings.HasPrefix(args.Destination, "https://hooks.slack.com/") {
			return "a slack destination must be an https://hooks.slack.com/ Incoming Webhook URL (or set slack_app_mode with a channel ID)"
		}
	case "email":
		for _, addr := range strings.Split(args.Destination, ",") {
			if a := strings.TrimSpace(addr); !strings.Contains(a, "@") || strings.ContainsAny(a, " \t") {
				return "an email destination must be comma-separated email addresses"
			}
		}
	case "generic_webhook":
		if !strings.HasPrefix(args.Destination, "https://") {
			return "a generic_webhook destination must be an HTTPS URL"
		}
	}
	return ""
}

func validateWebhookCACertificate(cert string) string {
	if cert == "" {
		return ""
	}
	if len(cert) > maxWebhookCACertificateBytes {
		return "webhook_ca_certificate exceeds the 64 KiB limit"
	}
	block, rest := pem.Decode([]byte(cert))
	if block == nil || !strings.HasPrefix(cert, "-----BEGIN ") || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return "webhook_ca_certificate must be a single PEM CERTIFICATE block starting with -----BEGIN CERTIFICATE-----"
	}
	if strings.TrimSpace(string(rest)) != "" {
		return "webhook_ca_certificate must contain exactly one certificate with no extra text"
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return "webhook_ca_certificate is not a valid X.509 certificate"
	}
	return ""
}

func validMentionHandles(handles string) bool {
	for _, h := range strings.Split(handles, ",") {
		parts := strings.Split(strings.TrimSpace(h), "|")
		if len(parts) == 1 {
			continue
		}
		var ok bool
		switch parts[0] {
		case "user":
			ok = len(parts) == 3 && slackUserIDPattern.MatchString(parts[1])
		case "usergroup":
			ok = len(parts) == 3 && slackUsergroupIDPattern.MatchString(parts[1])
		case "special":
			ok = len(parts) == 2 && (parts[1] == "here" || parts[1] == "channel")
		}
		if !ok {
			return false
		}
	}
	return true
}
