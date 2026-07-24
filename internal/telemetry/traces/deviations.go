package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	attributeDeviationsEndpoint = "/cat/api/traces/v2/attribute-deviations"
	attributeDeviationsVersion  = "trace-attribute-deviations/v1"
	maxDeviationWindow          = 15 * time.Minute
)

type GetTraceAttributeDeviationsArgs struct {
	ComparisonMode      string                   `json:"comparison_mode" jsonschema:"(Required) Cohort comparison: latency, errors, or time"`
	ServiceName         string                   `json:"service_name" jsonschema:"(Required) Exact service name to analyze"`
	Environment         string                   `json:"environment" jsonschema:"(Required) Exact deployment.environment value"`
	Operation           string                   `json:"operation,omitempty" jsonschema:"Optional exact span/operation name"`
	Filters             []map[string]interface{} `json:"filters,omitempty" jsonschema:"Optional additional trace filter conditions in trace JSON operator form"`
	CandidateAttributes []string                 `json:"candidate_attributes,omitempty" jsonschema:"Candidate attributes from get_trace_attributes_for_pipeline; maximum 8. Omit for bounded safe discovery."`
	LatencyThresholdMs  float64                  `json:"latency_threshold_ms,omitempty" jsonschema:"Latency split in milliseconds; required for latency mode"`
	StartTimeISO        string                   `json:"start_time_iso,omitempty" jsonschema:"Current/analysis window start in RFC3339"`
	EndTimeISO          string                   `json:"end_time_iso,omitempty" jsonschema:"Current/analysis window end in RFC3339"`
	LookbackMinutes     int                      `json:"lookback_minutes,omitempty" jsonschema:"Lookback ending now; default 15, maximum 15"`
	BaselineStartISO    string                   `json:"baseline_start_time_iso,omitempty" jsonschema:"(Required for time mode) Equal-duration baseline start in RFC3339"`
	BaselineEndISO      string                   `json:"baseline_end_time_iso,omitempty" jsonschema:"(Required for time mode) Equal-duration baseline end in RFC3339"`
	MinimumCohortSize   int                      `json:"minimum_cohort_size,omitempty" jsonschema:"Minimum spans required in each cohort; default 100, minimum 20"`
	MinimumValueSupport int                      `json:"minimum_value_support,omitempty" jsonschema:"Minimum pooled observations for a ranked value; default 20, minimum 10"`
	Limit               int                      `json:"limit,omitempty" jsonschema:"Maximum ranked deviations; default 10, maximum 10"`
}

type deviationAPIRequest struct {
	ContractVersion string                 `json:"contract_version"`
	Scope           deviationAPIScope      `json:"scope"`
	Comparison      deviationAPIComparison `json:"comparison"`
	Candidates      deviationAPICandidates `json:"candidates"`
	Limits          deviationAPILimits     `json:"limits"`
}

type deviationAPIScope struct {
	ServiceName string                   `json:"service_name"`
	Environment string                   `json:"environment"`
	Operation   string                   `json:"operation,omitempty"`
	Filters     []map[string]interface{} `json:"filters,omitempty"`
}

type deviationAPIComparison struct {
	Mode               string             `json:"mode"`
	Target             deviationAPIWindow `json:"target"`
	Control            deviationAPIWindow `json:"control"`
	LatencyThresholdMs *float64           `json:"latency_threshold_ms"`
}

type deviationAPIWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type deviationAPICandidates struct {
	Attributes   []string `json:"attributes"`
	AutoDiscover bool     `json:"auto_discover"`
}

type deviationAPILimits struct {
	MinimumCohortSize         int `json:"minimum_cohort_size"`
	MinimumValueSupport       int `json:"minimum_value_support"`
	MaximumCandidates         int `json:"maximum_candidates"`
	MaximumValuesPerAttribute int `json:"maximum_values_per_attribute"`
	MaximumRankedResults      int `json:"maximum_ranked_results"`
	RepresentativesPerResult  int `json:"representatives_per_result"`
}

func NewGetTraceAttributeDeviationsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceAttributeDeviationsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTraceAttributeDeviationsArgs) (*mcp.CallToolResult, any, error) {
		request, err := buildDeviationAPIRequest(args, time.Now())
		if err != nil {
			return nil, nil, err
		}
		body, err := callAttributeDeviationsAPI(ctx, client, cfg, request)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

func buildDeviationAPIRequest(args GetTraceAttributeDeviationsArgs, now time.Time) (deviationAPIRequest, error) {
	if strings.TrimSpace(args.ServiceName) == "" || strings.TrimSpace(args.Environment) == "" {
		return deviationAPIRequest{}, fmt.Errorf("service_name and environment are required")
	}
	mode := strings.ToLower(strings.TrimSpace(args.ComparisonMode))
	if mode != "latency" && mode != "errors" && mode != "time" {
		return deviationAPIRequest{}, fmt.Errorf("comparison_mode must be latency, errors, or time")
	}
	target, err := deviationTargetWindow(args, now)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	control, err := deviationControlWindow(args, mode, target)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	threshold, err := deviationLatencyThreshold(mode, args.LatencyThresholdMs)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	return newDeviationAPIRequest(args, mode, target, control, threshold), nil
}

func deviationTargetWindow(args GetTraceAttributeDeviationsArgs, now time.Time) (deviationAPIWindow, error) {
	params := map[string]interface{}{}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	if args.LookbackMinutes > 0 {
		params["lookback_minutes"] = args.LookbackMinutes
	}
	start, end, err := utils.GetTimeRangeAt(params, 15, now)
	if err != nil {
		return deviationAPIWindow{}, err
	}
	if end.Sub(start) > maxDeviationWindow {
		return deviationAPIWindow{}, fmt.Errorf("effective window must not exceed 15 minutes")
	}
	return deviationAPIWindow{Start: start, End: end}, nil
}

func deviationControlWindow(args GetTraceAttributeDeviationsArgs, mode string, target deviationAPIWindow) (deviationAPIWindow, error) {
	if mode != "time" {
		return target, nil
	}
	start, err := time.Parse(time.RFC3339, args.BaselineStartISO)
	if err != nil {
		return deviationAPIWindow{}, fmt.Errorf("baseline_start_time_iso must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, args.BaselineEndISO)
	if err != nil || !end.After(start) {
		return deviationAPIWindow{}, fmt.Errorf("baseline_end_time_iso must be RFC3339 and after baseline_start_time_iso")
	}
	control := deviationAPIWindow{Start: start, End: end}
	if end.Sub(start) != target.End.Sub(target.Start) {
		return deviationAPIWindow{}, fmt.Errorf("baseline and target windows must be equal in duration")
	}
	if start.Before(target.End) && target.Start.Before(end) {
		return deviationAPIWindow{}, fmt.Errorf("baseline and target windows must not overlap")
	}
	return control, nil
}

func deviationLatencyThreshold(mode string, value float64) (*float64, error) {
	if mode == "latency" {
		if value <= 0 {
			return nil, fmt.Errorf("latency_threshold_ms must be positive for latency mode")
		}
		return &value, nil
	}
	if value != 0 {
		return nil, fmt.Errorf("latency_threshold_ms is only valid for latency mode")
	}
	return nil, nil
}

func newDeviationAPIRequest(args GetTraceAttributeDeviationsArgs, mode string, target, control deviationAPIWindow, threshold *float64) deviationAPIRequest {
	return deviationAPIRequest{
		ContractVersion: attributeDeviationsVersion,
		Scope:           deviationAPIScope{ServiceName: args.ServiceName, Environment: args.Environment, Operation: args.Operation, Filters: args.Filters},
		Comparison:      deviationAPIComparison{Mode: mode, Target: target, Control: control, LatencyThresholdMs: threshold},
		Candidates:      deviationAPICandidates{Attributes: args.CandidateAttributes, AutoDiscover: len(args.CandidateAttributes) == 0},
		Limits:          deviationAPILimits{MinimumCohortSize: args.MinimumCohortSize, MinimumValueSupport: args.MinimumValueSupport, MaximumCandidates: len(args.CandidateAttributes), MaximumRankedResults: args.Limit},
	}
}

func callAttributeDeviationsAPI(ctx context.Context, client *http.Client, cfg models.Config, payload deviationAPIRequest) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("region", cfg.Region)
	endpoint := cfg.APIBaseURL + attributeDeviationsEndpoint + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	request.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
	request.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+cfg.TokenManager.GetAccessToken(ctx))
	request.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("attribute deviations API request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attribute deviations API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if !json.Valid(responseBody) {
		return nil, fmt.Errorf("attribute deviations API returned invalid JSON")
	}
	return responseBody, nil
}
