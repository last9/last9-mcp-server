package alerting

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type eofTransport struct{}

func (eofTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, io.EOF }

func eofClient() *http.Client { return &http.Client{Transport: eofTransport{}} }

func TestWriteToolsTransportEOFIsNotSuccess(t *testing.T) {
	cfg := testAlertMutationConfig("http://unused.invalid")
	ctx := context.Background()
	name := "latency"
	until := int64(1900000000)
	disabled := true
	for _, tt := range []struct {
		name string
		call func() (*mcp.CallToolResult, error)
	}{
		{"create_alert", func() (*mcp.CallToolResult, error) {
			r, _, err := NewCreateAlertHandler(eofClient(), cfg)(ctx, nil, CreateAlertArgs{EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
			return r, err
		}},
		{"update_alert", func() (*mcp.CallToolResult, error) {
			r, _, err := NewUpdateAlertHandler(eofClient(), cfg)(ctx, nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
			return r, err
		}},
		{"patch_alert", func() (*mcp.CallToolResult, error) {
			r, _, err := NewPatchAlertHandler(eofClient(), cfg)(ctx, nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &disabled})
			return r, err
		}},
		{"mute_alert_group", func() (*mcp.CallToolResult, error) {
			r, _, err := NewMuteAlertGroupHandler(eofClient(), cfg)(ctx, nil, MuteAlertGroupArgs{EntityID: "entity-1", Until: until})
			return r, err
		}},
		{"acknowledge_alert", func() (*mcp.CallToolResult, error) {
			r, _, err := NewAcknowledgeAlertHandler(eofClient(), cfg)(ctx, nil, AcknowledgeAlertArgs{AlertGroupID: "g", RuleID: "r", RuleType: "static"})
			return r, err
		}},
		{"reassign_alert", func() (*mcp.CallToolResult, error) {
			r, _, err := NewReassignAlertHandler(eofClient(), cfg)(ctx, nil, ReassignAlertArgs{AlertGroupID: "g", RuleID: "r", RuleType: "static", OwnerEmail: "a@example.com"})
			return r, err
		}},
		{"attach_notification_channel", func() (*mcp.CallToolResult, error) {
			r, _, err := NewAttachNotificationChannelHandler(eofClient(), cfg)(ctx, nil, AttachNotificationChannelArgs{ChannelID: 1, AlertGroupID: "g", Severity: "breach"})
			return r, err
		}},
		{"detach_notification_channel", func() (*mcp.CallToolResult, error) {
			r, _, err := NewDetachNotificationChannelHandler(eofClient(), cfg)(ctx, nil, DetachNotificationChannelArgs{AttachmentID: 1})
			return r, err
		}},
		{"create_notification_channel", func() (*mcp.CallToolResult, error) {
			r, _, err := NewCreateNotificationChannelHandler(eofClient(), cfg)(ctx, nil, CreateNotificationChannelArgs{Name: "n", Type: "slack", Destination: "https://hooks.slack.com/services/x"})
			return r, err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.call()
			if err == nil && (result == nil || !result.IsError) {
				t.Fatalf("transport EOF reported as success: %s", resultText(result))
			}
			if result != nil && strings.Contains(resultText(result), `"deleted":true`) {
				t.Fatalf("unexpected success payload: %s", resultText(result))
			}
		})
	}
}
