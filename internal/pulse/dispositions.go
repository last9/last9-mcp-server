package pulse

import (
	"context"
	"fmt"
	"net/http"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type WriteDispositionArgs struct {
	FindingID       string `json:"finding_id" jsonschema:"(Required) Canonical finding ID from get_pulse_finding."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"(Required) Current disposition_version; stale writes return HTTP 409."`
	Disposition     string `json:"disposition" jsonschema:"(Required) One of tune, expected, or investigate. Investigate records intent only."`
	Rationale       string `json:"rationale,omitempty" jsonschema:"Operator rationale retained in disposition history."`
	Confirmed       bool   `json:"confirmed" jsonschema:"(Required) Must be true after the user confirms this write."`
}

func NewWriteDispositionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, WriteDispositionArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args WriteDispositionArgs) (*mcp.CallToolResult, any, error) {
		id, err := validateDisposition(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodPatch, path: "/findings/" + id + "/disposition", body: dispositionPayload(args)})
		return handlerResult(body, err)
	}
}

func validateDisposition(args WriteDispositionArgs) (string, error) {
	if !args.Confirmed {
		return "", fmt.Errorf("confirmed must be true after the user approves this write")
	}
	if args.Disposition != "tune" && args.Disposition != "expected" && args.Disposition != "investigate" {
		return "", fmt.Errorf("disposition must be tune, expected, or investigate")
	}
	return escapedID(args.FindingID)
}

func dispositionPayload(args WriteDispositionArgs) map[string]any {
	return map[string]any{
		"expected_version": args.ExpectedVersion,
		"disposition":      args.Disposition,
		"rationale":        args.Rationale,
	}
}
