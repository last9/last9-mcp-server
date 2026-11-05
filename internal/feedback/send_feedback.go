package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"last9-mcp/internal/last9api"
	apifeedback "last9-mcp/internal/last9api/feedback"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Limits mirror the API's, so an oversized report fails here with the field named.
const (
	maxSummaryRunes  = 300
	maxDetailsRunes  = 4000
	maxToolNameRunes = 100
)

var categories = []string{"bug", "wrong_result", "missing_capability", "confusing_description", "slow", "praise", "other"}

type SendFeedbackArgs struct {
	Category string `json:"category" jsonschema:"(Required) One of: bug, wrong_result, missing_capability, confusing_description, slow, praise, other"`
	Summary  string `json:"summary" jsonschema:"(Required) One sentence, at most 300 characters, naming what happened and what you expected"`
	Details  string `json:"details,omitempty" jsonschema:"Optional detail, at most 4000 characters: the arguments you sent, the error or output you got, what you tried next. No secrets, tokens or customer data"`
	ToolName string `json:"tool_name,omitempty" jsonschema:"Optional name of the Last9 tool the feedback is about, e.g. get_logs"`
}

type sendFeedbackResult struct {
	FeedbackID string `json:"feedback_id"`
	Message    string `json:"message"`
}

func NewSendFeedbackHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, SendFeedbackArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SendFeedbackArgs) (*mcp.CallToolResult, any, error) {
		in := feedbackInput(ctx, args, req, cfg.ServerVersion)
		if msg := invalidFeedbackReason(in); msg != "" {
			return utils.ToolErrorResult(msg), nil, nil
		}
		out, err := apifeedback.SendMCPFeedback(ctx, last9api.NewClient(client, cfg), in)
		if err != nil {
			if result := upstreamFailureResult(err); result != nil {
				return result, nil, nil
			}
			return nil, nil, err
		}
		body, err := json.Marshal(sendFeedbackResult{
			FeedbackID: out.FeedbackID,
			Message:    "Feedback sent to the Last9 team. Continue with the user's task; do not send the same feedback again.",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("encode feedback result: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

// Client identity comes from clientInfo, never from arguments, so the team can tell which
// agents hit a problem. On --http it may come from _meta or the initialize session ID.
func feedbackInput(ctx context.Context, args SendFeedbackArgs, req *mcp.CallToolRequest, serverVersion string) apifeedback.SendMCPFeedbackInput {
	in := apifeedback.SendMCPFeedbackInput{
		Category:      strings.ToLower(strings.TrimSpace(args.Category)),
		Summary:       strings.TrimSpace(args.Summary),
		Details:       strings.TrimSpace(args.Details),
		ToolName:      strings.TrimSpace(args.ToolName),
		ServerVersion: serverVersion,
	}
	if info := utils.RequestClientInfo(ctx, req); info != nil {
		in.ClientName, in.ClientVersion = truncateRunes(info.Name, maxToolNameRunes), truncateRunes(info.Version, maxToolNameRunes)
	}
	return in
}

// upstreamFailureResult replaces the shared HTTP advice, written for queries ("retry later",
// "narrow the time range"), with the one rule for feedback: report once. Unknown outcomes
// are left to the call-deadline wrapper.
func upstreamFailureResult(err error) *mcp.CallToolResult {
	var unknown *utils.WriteOutcomeUnknownError
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &unknown) || !errors.As(err, &upstream) {
		return nil
	}
	reason := ""
	switch upstream.StatusCode {
	case http.StatusTooManyRequests:
		reason = " (the feedback rate limit is reached)"
	case http.StatusNotFound:
		reason = " (feedback may be unavailable or disabled)"
	}
	result := utils.ToolErrorResult(fmt.Sprintf("send mcp feedback failed with HTTP %d%s. Do not send the same feedback again; continue with the user's task.", upstream.StatusCode, reason))
	if utils.IsSystemError(err) {
		result.Meta = mcp.Meta{utils.ErrorTypeMetaKey: utils.SystemErrorType}
	}
	return result
}

func invalidFeedbackReason(in apifeedback.SendMCPFeedbackInput) string {
	switch {
	case !slices.Contains(categories, in.Category):
		return fmt.Sprintf("category must be one of: %s", strings.Join(categories, ", "))
	case in.Summary == "":
		return "summary is required"
	case utf8.RuneCountInString(in.Summary) > maxSummaryRunes:
		return fmt.Sprintf("summary exceeds %d characters; move the rest into details", maxSummaryRunes)
	case utf8.RuneCountInString(in.Details) > maxDetailsRunes:
		return fmt.Sprintf("details exceeds %d characters; shorten it", maxDetailsRunes)
	case utf8.RuneCountInString(in.ToolName) > maxToolNameRunes:
		return fmt.Sprintf("tool_name exceeds %d characters", maxToolNameRunes)
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
