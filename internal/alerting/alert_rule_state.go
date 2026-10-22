package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

type AlertRuleStateRequest struct {
	AlertGroupID   string       `json:"alert_group_id,omitempty" jsonschema:"Optional filter by alert group ID"`
	RuleName       string       `json:"rule_name,omitempty" jsonschema:"Optional regex filter by rule name"`
	AlertGroupName string       `json:"alert_group_name,omitempty" jsonschema:"Optional regex filter by alert group name"`
	LabelFilters   string       `json:"label_filters,omitempty" jsonschema:"Optional comma separated key-value label filters"`
	State          string       `json:"state,omitempty" jsonschema:"Optional state filter (e.g. firing)"`
	StartTime      TimestampArg `json:"start_time" jsonschema:"(Required) Start of the range (inclusive): Unix epoch seconds like 1718000000, or RFC3339 like 2024-06-10T06:13:20Z. Epoch milliseconds, microseconds and nanoseconds are converted to seconds."`
	EndTime        TimestampArg `json:"end_time" jsonschema:"(Required) End of the range (inclusive), in the same formats as start_time"`
	Step           int64        `json:"step" jsonschema:"(Required) Resolution step in seconds"`
}

// TimestampArg is a time argument as the caller sent it: a JSON integer or string.
// The handler parses it, so a bad value gets an error naming the argument.
type TimestampArg string

func (a *TimestampArg) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = TimestampArg(s)
		return nil
	}
	*a = TimestampArg(data)
	return nil
}

// AlertRuleStateInputSchema is the inferred schema with start_time and end_time
// widened to integer or string, which inference cannot express.
func AlertRuleStateInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[AlertRuleStateRequest](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[TimestampArg](): {Types: []string{"integer", "string"}},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("get_alert_rule_state input schema: %v", err))
	}
	return schema
}

const (
	alertRuleStateMaxPoints   = 100
	alertRuleStateConcurrency = 5
)

// alertRuleStateTimeout bounds the whole sampling run well below the clients'
// 3-minute tool-call timeout, so a slow upstream gets guidance instead of silence.
var alertRuleStateTimeout = 60 * time.Second

// earliestAlertTime (2000-01-01) rejects small numbers, which are durations or
// relative offsets rather than timestamps.
var earliestAlertTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

const timestampFormatHint = "Use Unix epoch seconds like 1718000000 or RFC3339 like 2024-06-10T06:13:20Z; get_current_time returns the current time in both"

// parseTimestampArg returns arg as Unix seconds. Integers of 13, 16 or 19 digits
// are read as epoch milliseconds, microseconds or nanoseconds.
func parseTimestampArg(name string, arg TimestampArg) (int64, error) {
	raw := strings.TrimSpace(string(arg))
	if raw == "" {
		return 0, fmt.Errorf("%s is required. %s", name, timestampFormatHint)
	}
	var sec int64
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		sec = epochSeconds(n)
	} else if t, err := utils.ParseToolTimestamp(raw); err == nil {
		sec = t.Unix()
	} else {
		return 0, fmt.Errorf("%s %q is not a timestamp. %s", name, raw, timestampFormatHint)
	}
	if sec < earliestAlertTime {
		return 0, fmt.Errorf("%s %q is before 2000-01-01: pass an absolute time, not a duration or offset. %s", name, raw, timestampFormatHint)
	}
	return sec, nil
}

func epochSeconds(n int64) int64 {
	switch {
	case n >= 1e17:
		return n / 1e9
	case n >= 1e14:
		return n / 1e6
	case n >= 1e11:
		return n / 1e3
	default:
		return n
	}
}

func NewAlertRuleStateHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return func(ctx context.Context, req *mcp.CallToolRequest, args AlertRuleStateRequest) (*mcp.CallToolResult, any, error) {
		startTime, err := parseTimestampArg("start_time", args.StartTime)
		if err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		endTime, err := parseTimestampArg("end_time", args.EndTime)
		if err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		if startTime >= endTime {
			return utils.ToolErrorResult("start_time must be less than end_time"), nil, nil
		}
		if args.Step <= 0 {
			return utils.ToolErrorResult("step must be greater than 0"), nil, nil
		}

		// Cannot overflow: parseTimestampArg bounds both ends to [2000-01-01, ~2262].
		span := endTime - startTime

		// Inclusive sample count: t iterates start, start+step, ..., end.
		// Compare before the +1 so the count can never wrap: with span >= 0 and
		// step >= 1, span/step itself cannot overflow.
		if span/args.Step >= alertRuleStateMaxPoints {
			minStep := span/alertRuleStateMaxPoints + 1
			return utils.ToolErrorResult(fmt.Sprintf("time range and step result in too many points. Maximum is %d samples; set step to at least %d seconds or narrow the range", alertRuleStateMaxPoints, minStep)), nil, nil
		}
		points := span/args.Step + 1

		type Datapoint struct {
			Timestamp int64 `json:"timestamp"`
			IsFiring  int   `json:"is_firing"`
		}

		sampler := ruleStateSampler{client: client, cfg: cfg, args: args,
			// One monitor call answers for at most MaxAlertsMonitorWindowSeconds.
			window: min(args.Step, MaxAlertsMonitorWindowSeconds)}
		samples := make([][]ruleState, points)
		var fetched atomic.Int64

		sampleCtx, cancel := context.WithTimeout(ctx, alertRuleStateTimeout)
		defer cancel()
		g, gctx := errgroup.WithContext(sampleCtx)
		g.SetLimit(alertRuleStateConcurrency)
		// Index-bounded by the validated `points` so the loop terminates by
		// construction; a value-bounded `t <= end_time` loop can run unbounded
		// when the span arithmetic overflows.
		for i := int64(0); i < points; i++ {
			g.Go(func() error {
				rules, err := sampler.fetch(gctx, startTime+i*args.Step)
				if err != nil {
					return err
				}
				samples[i] = rules
				fetched.Add(1)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			if ctx.Err() == nil && errors.Is(sampleCtx.Err(), context.DeadlineExceeded) {
				return utils.ToolErrorResult(fmt.Sprintf(
					"get_alert_rule_state timed out after %s with %d of %d samples fetched; each sample is one upstream call. Retry with fewer samples: increase step or narrow start_time/end_time, and filter by alert_group_id or rule_name.",
					alertRuleStateTimeout, fetched.Load(), points)), nil, nil
			}
			return nil, nil, err
		}

		// results is a map of rule_id -> timestamp -> is_firing
		results := make(map[string]map[int64]int)
		for i, rules := range samples {
			t := startTime + int64(i)*args.Step
			for _, rule := range rules {
				if _, exists := results[rule.ruleID]; !exists {
					results[rule.ruleID] = make(map[int64]int)
				}
				if rule.firing {
					results[rule.ruleID][t] = 1
				} else {
					results[rule.ruleID][t] = 0
				}
			}
		}

		// Construct final continuous timeseries for all seen rules. Gaps are zero-filled; see tool description for caveats.
		finalResults := make(map[string][]Datapoint)
		for ruleID, tsMap := range results {
			var dps []Datapoint
			for i := int64(0); i < points; i++ {
				t := startTime + i*args.Step
				dps = append(dps, Datapoint{
					Timestamp: t,
					IsFiring:  tsMap[t],
				})
			}
			finalResults[ruleID] = dps
		}

		formattedBytes, err := json.MarshalIndent(finalResults, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal results: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(formattedBytes),
				},
			},
		}, nil, nil
	}
}

type ruleStateSampler struct {
	client *http.Client
	cfg    models.Config
	args   AlertRuleStateRequest
	window int64
}

// ruleState is all a sample keeps of a rule, so completed samples do not hold
// annotations and labels on the heap while the rest are fetched.
type ruleState struct {
	ruleID string
	firing bool
}

func (s ruleStateSampler) fetch(ctx context.Context, t int64) ([]ruleState, error) {
	args, cfg := s.args, s.cfg
	queryParams := url.Values{}
	queryParams.Set("timestamp", fmt.Sprintf("%d", t))
	queryParams.Set("window", fmt.Sprintf("%d", s.window))

	if args.AlertGroupID != "" {
		queryParams.Set("alert_group_id", args.AlertGroupID)
	}
	if args.RuleName != "" {
		queryParams.Set("rule_name", args.RuleName)
	}
	if args.AlertGroupName != "" {
		queryParams.Set("alert_group_name", args.AlertGroupName)
	}
	if args.LabelFilters != "" {
		queryParams.Set("label_filters", args.LabelFilters)
	}
	if args.State != "" {
		queryParams.Set("state", args.State)
	}

	finalURL := fmt.Sprintf("%s%s?%s", cfg.APIBaseURL, constants.EndpointAlertsMonitor, queryParams.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for timestamp %d: %w", t, err)
	}

	httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	if cfg.TokenManager != nil {
		// Fetch per-sample so token-manager refresh logic stays in effect for long ranges.
		if token := cfg.TokenManager.GetAccessToken(ctx); token != "" {
			httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
		}
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed at timestamp %d: %w", t, err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read response body at timestamp %d: %w", t, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned error status %d at timestamp %d: %s", resp.StatusCode, t, string(body))
	}

	var alertResp AlertsResponse
	if err := json.Unmarshal(body, &alertResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alerts response at timestamp %d: %w", t, err)
	}
	states := make([]ruleState, len(alertResp.AlertRules))
	for i, rule := range alertResp.AlertRules {
		states[i] = ruleState{ruleID: rule.RuleID, firing: rule.State == "firing"}
	}
	return states, nil
}
