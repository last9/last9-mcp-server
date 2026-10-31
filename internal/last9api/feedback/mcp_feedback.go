package feedback

import (
	"context"
	"net/http"

	"last9-mcp/internal/last9api"
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
	err := c.Do(ctx, "send mcp feedback", last9api.Request{Method: http.MethodPost, Path: pathMCPFeedback, Body: in}, &out)
	return out, err
}
