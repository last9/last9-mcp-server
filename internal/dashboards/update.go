package dashboards

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewUpdateDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateDashboardArgs) (*mcp.CallToolResult, any, error) {
		if err := validateID(args.ID); err != nil {
			return nil, nil, err
		}
		if err := validateDashboardRequest(args.Dashboard); err != nil {
			return nil, nil, err
		}
		if err := validateMetadata(args.Metadata); err != nil {
			return nil, nil, err
		}

		payload, err := marshalDashboardRequest(args.DashboardRequest)
		if err != nil {
			return nil, nil, err
		}

		lock, err := readDashboardLock(ctx, client, cfg, args.ID)
		if err != nil {
			return nil, nil, err
		}
		if lock.locked() {
			return lockedResult(lock), nil, nil
		}

		path := fmt.Sprintf(constants.EndpointDashboardByID, url.PathEscape(args.ID))
		u := cfg.APIBaseURL + path
		body, status, err := doJSONRequest(ctx, client, cfg, http.MethodPut, u, payload)
		if err != nil && isDashboardLockedRefusal(status, string(body)) {
			return lockedResult(lock), nil, nil
		}
		if err != nil && status == http.StatusForbidden {
			return forbiddenWriteResult(err), nil, nil
		}
		if err != nil {
			return nil, nil, mapDashboardAPIError(err)
		}

		return textResultWithDashboardLink(dlBuilder, body, args.ID), nil, nil
	}
}
