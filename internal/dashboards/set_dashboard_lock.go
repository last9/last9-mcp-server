package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SetDashboardLockArgs struct {
	ID     string `json:"id" jsonschema:"(Required) Dashboard UUID"`
	Locked *bool  `json:"locked" jsonschema:"(Required) true locks the dashboard against edits and deletes; false unlocks it"`
}

func NewSetDashboardLockHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, SetDashboardLockArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SetDashboardLockArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}
		if args.Locked == nil {
			return nil, nil, errors.New("locked is required: true to lock, false to unlock")
		}

		resp, err := apidashboards.SetDashboardReadonly(ctx, last9api.NewClient(client, cfg), args.ID, *args.Locked)
		if err != nil {
			return nil, nil, mapDashboardLockError(args.ID, err)
		}
		if resp.Readonly == nil {
			return nil, nil, &utils.WriteOutcomeUnknownError{
				Cause: "the response did not report the lock state",
				Err:   errors.New("set dashboard lock response did not report the lock state; get_dashboard shows whether it changed"),
			}
		}

		body, err := json.Marshal(struct {
			ID     string `json:"id"`
			Locked bool   `json:"locked"`
		}{args.ID, *resp.Readonly})
		if err != nil {
			return nil, nil, err
		}
		return textResultWithDashboardLink(dlBuilder, body, args.ID), nil, nil
	}
}

func mapDashboardLockError(id string, err error) error {
	var unknown *utils.WriteOutcomeUnknownError
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &unknown) || !errors.As(err, &upstream) {
		return err
	}
	switch upstream.StatusCode {
	case http.StatusForbidden:
		if strings.Contains(strings.ToLower(upstream.Body), "built-in") {
			return errors.New("built-in dashboards are always read-only and cannot be locked or unlocked")
		}
		return errors.New("only the author or an org admin can lock or unlock this dashboard")
	case http.StatusNotFound:
		return fmt.Errorf("dashboard %q not found; list_dashboards lists dashboard ids", id)
	default:
		return err
	}
}
