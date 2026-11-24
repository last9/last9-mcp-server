package apm

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/suggest"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultLogServicesLimit = 10
	maxLogServicesLimit     = 100
)

type GetLogServicesArgs struct {
	Query           string  `json:"query,omitempty" jsonschema:"Service name to rank by: each row gets a similarity percent (word order ignored) and rows sort by it, then name. Omitted sorts by name."`
	Env             string  `json:"env,omitempty" jsonschema:"Only services sending logs from this environment (exact)."`
	Index           string  `json:"index,omitempty" jsonschema:"Only services sending logs to this physical index name (exact, e.g. default)."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back from now (default: 60, minimum: 1). When no window is given and nothing is found, the lookup is retried once over the last 4 days."`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start of the window, RFC3339. Takes precedence over lookback_minutes."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End of the window, RFC3339 (default: now)."`
	Limit           *int    `json:"limit,omitempty" jsonschema:"Rows to return, 1-100 (default: 10). total always counts every matching service."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in an additional content block)."`
}

type logServiceRow struct {
	Name       string   `json:"name"`
	Envs       []string `json:"envs"`
	Indexes    []string `json:"indexes"`
	LastSeen   *string  `json:"last_seen"`
	Similarity *int     `json:"similarity,omitempty"`
	lastSeen   float64
}

type logServicesResult struct {
	Rows      []logServiceRow `json:"rows"`
	Total     int             `json:"total"`
	Returned  int             `json:"returned"`
	Truncated bool            `json:"truncated"`
	Note      string          `json:"note,omitempty"`
}

// NewGetLogServicesHandler lists services that send logs, from the log-index
// service count metric, so log-only services absent from APM are included.
func NewGetLogServicesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogServicesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogServicesArgs) (*mcp.CallToolResult, any, error) {
		limit := defaultLogServicesLimit
		if args.Limit != nil {
			limit = *args.Limit
		}
		if limit < 1 || limit > maxLogServicesLimit {
			return nil, nil, fmt.Errorf("limit must be between 1 and %d, got %d", maxLogServicesLimit, limit)
		}
		window, err := resolveLabelWindow(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}
		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}

		api := last9api.NewClient(client, choice.cfg)
		selector := logServicesSelector(strings.TrimSpace(args.Env), strings.TrimSpace(args.Index))
		fetch := func(start, end int64) ([]logServiceRow, error) {
			series, err := prom.QueryInstant(ctx, api, prom.QueryInstantInput{
				Datasource: promDatasource(choice.cfg),
				Query:      fmt.Sprintf("max by (service_name, env, name) (tlast_over_time(%s[%ds]))", selector, end-start),
				Timestamp:  end,
			})
			if err != nil {
				return nil, err
			}
			return groupLogServices(series), nil
		}

		rows, err := fetch(window.start, window.end)
		var upstream *utils.UpstreamHTTPError
		if errors.As(err, &upstream) {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		if err != nil {
			return nil, nil, err
		}

		var note string
		if len(rows) == 0 && window.isDefault {
			wide, err := fetch(window.end-sparseLookbackMinutes*60, window.end)
			switch {
			case err != nil:
				note = fmt.Sprintf("No log services in the last %d minutes, and retrying over the last 4 days failed, so discovery is incomplete: %v", utils.DefaultLookbackMinutes, err)
			case len(wide) == 0:
				note = fmt.Sprintf("No log services in the last %d minutes or the last 4 days. %s", utils.DefaultLookbackMinutes, logServicesAbsenceNote)
			default:
				rows = wide
				note = fmt.Sprintf("No log services in the last %d minutes; showing the last 4 days instead.", utils.DefaultLookbackMinutes)
			}
		} else if len(rows) == 0 {
			note = "No log services in this window. " + logServicesAbsenceNote
		}

		result := rankLogServices(rows, strings.TrimSpace(args.Query), limit)
		result.Note = note
		body, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return textResult(body, choice.note), nil, nil
	}
}

const logServicesAbsenceNote = "This is not proof a service sends no logs: the metric may not cover it, or env/index filters may exclude it; check with get_logs before saying it does not exist."

func logServicesSelector(env, index string) string {
	matchers := []string{`destination="logs"`}
	if env != "" {
		matchers = append(matchers, fmt.Sprintf("env=%q", env))
	}
	if index != "" {
		matchers = append(matchers, fmt.Sprintf("name=%q", index))
	}
	return "physical_index_service_count{" + strings.Join(matchers, ",") + "}"
}

// groupLogServices folds one series per service, env and index into one row
// per service. A series without a service name cannot be offered and is skipped.
func groupLogServices(series []prom.InstantSeries) []logServiceRow {
	byName := map[string]*logServiceRow{}
	var rows []*logServiceRow
	for _, s := range series {
		name := s.Metric["service_name"]
		if name == "" {
			continue
		}
		row, ok := byName[name]
		if !ok {
			row = &logServiceRow{Name: name, Envs: []string{}, Indexes: []string{}, lastSeen: math.NaN()}
			byName[name] = row
			rows = append(rows, row)
		}
		row.Envs = appendUnique(row.Envs, s.Metric["env"])
		row.Indexes = appendUnique(row.Indexes, s.Metric["name"])
		if ts, ok := sampleSeconds(s.Value); ok && !(ts <= row.lastSeen) {
			row.lastSeen = ts
		}
	}
	out := make([]logServiceRow, 0, len(rows))
	for _, row := range rows {
		slices.Sort(row.Envs)
		slices.Sort(row.Indexes)
		if !math.IsNaN(row.lastSeen) {
			seen := time.Unix(int64(row.lastSeen), 0).UTC().Format(time.RFC3339)
			row.LastSeen = &seen
		}
		out = append(out, *row)
	}
	return out
}

func appendUnique(list []string, v string) []string {
	if v == "" || slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// sampleSeconds reads an instant sample [time, "value"]; here the value is the
// Unix time of the series' last sample.
func sampleSeconds(value []any) (float64, bool) {
	if len(value) != 2 {
		return 0, false
	}
	s, ok := value[1].(string)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, false
	}
	return f, true
}

func rankLogServices(rows []logServiceRow, query string, limit int) logServicesResult {
	if query != "" {
		for i := range rows {
			p := suggest.NameSimilarity(query, rows[i].Name)
			rows[i].Similarity = &p
		}
	}
	slices.SortFunc(rows, func(a, b logServiceRow) int {
		if query != "" {
			if c := cmp.Compare(*b.Similarity, *a.Similarity); c != 0 {
				return c
			}
		}
		return strings.Compare(a.Name, b.Name)
	})
	returned := rows[:min(limit, len(rows))]
	return logServicesResult{
		Rows:      returned,
		Total:     len(rows),
		Returned:  len(returned),
		Truncated: len(rows) > len(returned),
	}
}
