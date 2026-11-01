package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	previewInputBytes = 64 << 10
	previewMaxPanels  = 50
	previewMaxQueries = 100
	previewMaxOps     = 100
	previewTimeout    = time.Minute
)

type PreviewDashboardArgs struct {
	Dashboard    map[string]any     `json:"dashboard,omitempty" jsonschema:"Unsaved dashboard object as in create_dashboard; mutually exclusive with id"`
	ID           string             `json:"id,omitempty" jsonschema:"Saved dashboard ID for patch preview; requires ops and excludes dashboard"`
	Ops          []PatchDashboardOp `json:"ops,omitempty" jsonschema:"Ordered patch_dashboard operations; required with id, at most 100"`
	StartTimeISO string             `json:"start_time_iso" jsonschema:"(Required) Window start in RFC3339, maximum window 24 hours"`
	EndTimeISO   string             `json:"end_time_iso" jsonschema:"(Required) Window end in RFC3339, after start"`
	Variables    map[string]any     `json:"variables,omitempty" jsonschema:"Scalar variable overrides; defaults to dashboard values then built-in values"`
}

func NewPreviewDashboardHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PreviewDashboardArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args PreviewDashboardArgs) (*mcp.CallToolResult, any, error) {
		parsed, err := validatePreviewArgs(args)
		if err != nil {
			return nil, nil, err
		}
		// Patch operations can retain caller-owned maps; clone before applying later ops.
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, nil, err
		}
		var cloned PreviewDashboardArgs
		if err = json.Unmarshal(raw, &cloned); err != nil {
			return nil, nil, err
		}
		args = cloned
		ctx, cancel := context.WithTimeout(ctx, previewTimeout)
		defer cancel()
		if args.ID != "" {
			if _, err := resolveRegion(cfg, ""); err != nil {
				return nil, nil, err
			}
		}
		dashboard, diff, err := preparePreviewDashboard(ctx, last9api.NewClient(client, cfg), args)
		if err != nil {
			return nil, nil, err
		}
		parsed.dashboardID = nil
		parsed.definition = dashboard
		report, err := previewPanels(ctx, &validateExecutor{client: client, cfg: cfg}, parsed)
		if err != nil {
			return nil, nil, err
		}
		if diff != nil {
			report["diff"] = diff
		}
		raw, err = previewResult(report)
		if err != nil {
			return nil, nil, err
		}
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
		if args.ID != "" {
			result.Meta = deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildDashboardLink(args.ID))
		}
		return result, nil, nil
	}
}

func validatePreviewArgs(args PreviewDashboardArgs) (*validatedDashboardArgs, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if len(raw) > previewInputBytes {
		return nil, fmt.Errorf("preview input must be at most %d bytes", previewInputBytes)
	}
	if (args.ID != "") == (args.Dashboard != nil) {
		return nil, errors.New("provide exactly one of dashboard or id with ops")
	}
	if args.ID == "" && len(args.Ops) > 0 {
		return nil, errors.New("ops require id")
	}
	if len(args.Ops) > previewMaxOps {
		return nil, fmt.Errorf("preview supports at most %d ops", previewMaxOps)
	}
	if args.ID != "" {
		if err := validateID(args.ID); err != nil {
			return nil, err
		}
		if err := validatePatchOps(args.Ops); err != nil {
			return nil, err
		}
	}
	input := ValidateDashboardArgs{DashboardDefinition: args.Dashboard, StartTimeISO: args.StartTimeISO, EndTimeISO: args.EndTimeISO, Variables: args.Variables}
	if args.ID != "" {
		input.DashboardID = &args.ID
	}
	parsed, errs := validateDashboardInput(input)
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	if args.Dashboard != nil {
		if err := checkPreviewDashboardSize(args.Dashboard); err != nil {
			return nil, err
		}
	}
	return parsed, nil
}

func preparePreviewDashboard(ctx context.Context, api *last9api.Client, args PreviewDashboardArgs) (map[string]any, *previewDiff, error) {
	dashboard := args.Dashboard
	var before map[string]any
	var changes []patchChange
	if args.ID != "" {
		current, err := apidashboards.GetDashboard(ctx, api, args.ID)
		if err != nil {
			return nil, nil, err
		}
		if current.Dashboard == nil {
			return nil, nil, errors.New("get dashboard returned no dashboard object")
		}
		dashboard = current.Dashboard
		if err := checkPreviewDashboardSize(dashboard); err != nil {
			return nil, nil, err
		}
		if err := checkPreviewPanelIDs(dashboard); err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(dashboard)
		if err != nil {
			return nil, nil, err
		}
		if err = json.Unmarshal(raw, &before); err != nil {
			return nil, nil, err
		}
		changes, err = applyPatchOps(dashboard, args.Ops)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := checkPreviewDashboardSize(dashboard); err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(dashboard)
	if err != nil {
		return nil, nil, err
	}
	if args.ID == "" || len(changes) > 0 {
		raw, err = defaultPanelVersions(raw)
		if err != nil {
			return nil, nil, err
		}
	}
	if err = json.Unmarshal(raw, &dashboard); err != nil {
		return nil, nil, err
	}
	if err := checkPreviewDashboardSize(dashboard); err != nil {
		return nil, nil, err
	}
	if before == nil {
		return dashboard, nil, nil
	}
	return dashboard, previewPanelDiff(before, dashboard, changes), nil
}

func checkPreviewDashboardSize(dashboard map[string]any) error {
	raw, err := json.Marshal(dashboard)
	if err != nil {
		return err
	}
	if len(raw) > previewInputBytes {
		return fmt.Errorf("dashboard must be at most %d bytes for preview", previewInputBytes)
	}
	panels := asAnySlice(dashboard["panels"])
	if dashboard["panels"] != nil && panels == nil {
		return errors.New("dashboard.panels must be a list")
	}
	if len(panels) > previewMaxPanels {
		return fmt.Errorf("preview supports at most %d panels", previewMaxPanels)
	}
	queries := 0
	for _, raw := range panels {
		panel, _ := raw.(map[string]any)
		queries += len(asAnySlice(panel["queries"]))
	}
	if queries > previewMaxQueries {
		return fmt.Errorf("preview supports at most %d queries", previewMaxQueries)
	}
	return nil
}
