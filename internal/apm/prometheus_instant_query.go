package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromqlInstantQueryArgs struct {
	Query           string          `json:"query" jsonschema:"(Required) PromQL query to execute"`
	Time            json.RawMessage `json:"time,omitempty"`
	TimeISO         string          `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). If omitted, defaults to now or now-lookback_minutes."`
	LookbackMinutes float64         `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now when time_iso is omitted (default: 0, minimum: 1)."`
	Datasource      string          `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in a second content block)."`
}

// instantQueryLinkWindow is how far before the evaluation time the Metrics
// explorer link starts, so the chart shows the run-up to the instant value.
const instantQueryLinkWindow = 15 * 60

func resolveInstantQueryTime(timeISO string, lookbackMinutes float64) (int64, error) {
	if timeISO != "" {
		_, endTime, err := utils.GetTimeRange(map[string]interface{}{
			"end_time_iso": timeISO,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, fmt.Errorf("invalid time_iso format: %w", err)
		}
		return endTime.Unix(), nil
	}

	if lookbackMinutes != 0 {
		startTime, _, err := utils.GetTimeRange(map[string]interface{}{
			"lookback_minutes": lookbackMinutes,
		}, utils.DefaultLookbackMinutes)
		if err != nil {
			return 0, err
		}
		return startTime.Unix(), nil
	}

	return utils.Now().UTC().Unix(), nil
}

func (args PromqlInstantQueryArgs) evaluationTime() (int64, error) {
	if len(args.Time) == 0 {
		return resolveInstantQueryTime(args.TimeISO, args.LookbackMinutes)
	}
	value := string(args.Time)
	if args.Time[0] == '"' {
		if err := json.Unmarshal(args.Time, &value); err != nil {
			return 0, err
		}
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		// Round toward the past so a decimal just below a whole second stays in that second.
		numeric, _, err := big.ParseFloat(value, 10, 128, big.ToNegativeInf)
		if err != nil || numeric.IsInf() {
			return 0, errors.New("time must be RFC3339 or Unix seconds")
		}
		whole, accuracy := numeric.Int64()
		if whole < math.MinInt64/int64(time.Second) || whole > math.MaxInt64/int64(time.Second) {
			return 0, errors.New("time is outside the supported evaluation range")
		}
		if accuracy == big.Above {
			whole--
		}
		instant = time.Unix(whole, 0)
	}
	seconds := instant.Unix()
	if seconds == 0 || seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
		return 0, errors.New("time is outside the supported evaluation range or resolves to reserved Unix second zero")
	}
	if args.TimeISO != "" {
		canonical, err := utils.ParseToolTimestamp(args.TimeISO)
		if err != nil {
			return 0, err
		}
		if canonical.Unix() != seconds {
			return 0, errors.New("conflicting time and time_iso: supply the same evaluation second")
		}
	}
	return seconds, nil
}

func NewPromqlInstantQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		timeParam, err := args.evaluationTime()
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
		raw, err := prom.QueryInstantRaw(ctx, last9api.NewClient(client, choice.cfg), prom.QueryInstantInput{
			Datasource: promDatasource(choice.cfg),
			Query:      query,
			Timestamp:  timeParam,
		})
		res, out, err := promToolResult(raw, err, choice.note)
		return withMetricsLink(res, choice.cfg, metricsLink{StartSec: timeParam - instantQueryLinkWindow, EndSec: timeParam, Query: query}), out, err
	}
}
