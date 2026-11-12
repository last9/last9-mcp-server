package dashboards

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultListDashboardsLimit = 100
	maxListDashboardsLimit     = 200
	// listDashboardsResponseBound is the response size agents work within.
	listDashboardsResponseBound = 32 * 1024
	// listEnvelopeReserve covers the wrapper object, total and next_cursor around the items.
	listEnvelopeReserve = 256
)

type ListDashboardsArgs struct {
	Name   string `json:"name,omitempty" jsonschema:"Case-insensitive substring of the dashboard name; only matching dashboards are listed and counted"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"Dashboards per page, 1-200 (default 100); a page also ends early to stay under 32 KB"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page; pass the same name with it. Omit for the first page"`
}

type dashboardSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
}

type listDashboardsPage struct {
	Dashboards []dashboardSummary `json:"dashboards"`
	Total      int                `json:"total"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func NewListDashboardsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDashboardsArgs) (*mcp.CallToolResult, any, error) {
	dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListDashboardsArgs) (*mcp.CallToolResult, any, error) {
		limit, err := listDashboardsLimit(args.Limit)
		if err != nil {
			return nil, nil, err
		}
		name := strings.TrimSpace(args.Name)
		offset, err := decodeListCursor(args.Cursor, name)
		if err != nil {
			return nil, nil, err
		}

		u := cfg.APIBaseURL + constants.EndpointDashboards
		body, _, err := doJSONRequest(ctx, client, cfg, http.MethodGet, u, nil)
		if err != nil {
			return nil, nil, err
		}
		var all []dashboardSummary
		if err := json.Unmarshal(body, &all); err != nil {
			return nil, nil, fmt.Errorf("decode dashboard list: %w", err)
		}

		page, err := json.Marshal(paginateDashboards(filterDashboards(all, name), offset, limit, name))
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dlBuilder.BuildDashboardsIndexLink()),
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(page)},
			},
		}, nil, nil
	}
}

func listDashboardsLimit(limit *int) (int, error) {
	if limit == nil {
		return defaultListDashboardsLimit, nil
	}
	if *limit < 1 || *limit > maxListDashboardsLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d, got %d", maxListDashboardsLimit, *limit)
	}
	return *limit, nil
}

func filterDashboards(all []dashboardSummary, name string) []dashboardSummary {
	if name == "" {
		return all
	}
	needle := strings.ToLower(name)
	var matched []dashboardSummary
	for _, d := range all {
		if strings.Contains(strings.ToLower(d.Name), needle) {
			matched = append(matched, d)
		}
	}
	return matched
}

// paginateDashboards always takes at least one dashboard so paging advances.
func paginateDashboards(matched []dashboardSummary, offset, limit int, name string) listDashboardsPage {
	page := listDashboardsPage{Dashboards: []dashboardSummary{}, Total: len(matched)}
	size := listEnvelopeReserve
	end := min(offset, len(matched))
	for end < len(matched) && len(page.Dashboards) < limit {
		item, _ := json.Marshal(matched[end])
		size += len(item) + 1
		if size >= listDashboardsResponseBound && len(page.Dashboards) > 0 {
			break
		}
		page.Dashboards = append(page.Dashboards, matched[end])
		end++
	}
	if end < len(matched) {
		page.NextCursor = encodeListCursor(end, name)
	}
	return page
}

var (
	errMalformedListCursor = errors.New("cursor is malformed: pass next_cursor from list_dashboards unchanged, or omit cursor to start from the first page")
	errForeignListCursor   = errors.New("cursor was issued for a different name filter: pass the name used on the page that returned it, or omit cursor to start from the first page")
)

// A cursor is bound to its name filter, since an offset into one filtered list is meaningless in another.
func encodeListCursor(offset int, name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset) + ":" + nameFilterKey(name)))
}

func decodeListCursor(cursor, name string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errMalformedListCursor
	}
	offsetText, key, ok := strings.Cut(string(raw), ":")
	offset, err := strconv.Atoi(offsetText)
	if !ok || err != nil || offset < 0 {
		return 0, errMalformedListCursor
	}
	if key != nameFilterKey(name) {
		return 0, errForeignListCursor
	}
	return offset, nil
}

func nameFilterKey(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}
