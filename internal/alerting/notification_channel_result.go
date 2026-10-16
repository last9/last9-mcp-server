package alerting

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxChannelRejectionReasonRunes = 300

// Upstream bodies are never relayed: channel requests carry secrets that a rejection can echo.
// channelRejectionResult is the one exception, for the API's own 400/422 reason.
func notificationChannelErrorResult(err error, hints map[int]string) (*mcp.CallToolResult, any, error) {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) {
		return nil, nil, err
	}
	if hint, ok := hints[upstream.StatusCode]; ok {
		return utils.ToolErrorResult(hint), nil, nil
	}
	return utils.ToolErrorResult(fmt.Sprintf("the API returned HTTP %d", upstream.StatusCode)), nil, nil
}

// channelRejectionResult relays the `error` field of a 400/422 (e.g. that personal
// email addresses are refused) with every request secret redacted; ok is false when
// there is no such field, so callers fall back to their fixed hint.
func channelRejectionResult(err error, secrets []string) (res *mcp.CallToolResult, ok bool) {
	var upstream *utils.UpstreamHTTPError
	if !errors.As(err, &upstream) || (upstream.StatusCode != http.StatusBadRequest && upstream.StatusCode != http.StatusUnprocessableEntity) {
		return nil, false
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(upstream.Body), &body) != nil {
		return nil, false
	}
	reason := body.Error
	for _, secret := range secrets {
		if secret != "" {
			reason = strings.ReplaceAll(reason, secret, "[redacted]")
		}
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, false
	}
	if r := []rune(reason); len(r) > maxChannelRejectionReasonRunes {
		reason = string(r[:maxChannelRejectionReasonRunes]) + "…"
	}
	return utils.ToolErrorResult("the API rejected the channel: " + reason), true
}

func notificationChannelJSONResult(cfg models.Config, v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	link := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildNotificationChannelsLink()
	return &mcp.CallToolResult{Meta: deeplink.ToMeta(link), Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}
