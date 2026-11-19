package feedback

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/utils"
)

const pathMCPFeedback = "/mcp_feedback"

type SendMCPFeedbackInput struct {
	Category      string `json:"category"`
	Summary       string `json:"summary"`
	Details       string `json:"details,omitempty"`
	ToolName      string `json:"tool_name,omitempty"`
	ClientName    string `json:"client_name,omitempty"`
	ClientVersion string `json:"client_version,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
}

type SendMCPFeedbackResponse struct {
	FeedbackID string `json:"feedback_id"`
}

// SendMCPFeedback calls POST /mcp_feedback.
func SendMCPFeedback(ctx context.Context, c *last9api.Client, in SendMCPFeedbackInput) (SendMCPFeedbackResponse, error) {
	var out SendMCPFeedbackResponse
	err := c.Do(ctx, "send mcp feedback", last9api.Request{Method: http.MethodPost, Path: pathMCPFeedback, Body: in}, &out, last9api.WithRequiredBody())
	if err == nil && strings.TrimSpace(out.FeedbackID) == "" {
		err = &utils.WriteOutcomeUnknownError{
			Cause: "the API response omitted the feedback ID",
			Err:   errors.New("send mcp feedback: response missing feedback_id; delivery is unconfirmed, do not send the same feedback again"),
		}
	}
	return out, err
}
