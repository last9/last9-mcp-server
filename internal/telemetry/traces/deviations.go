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

// Request bounds, mirroring the endpoint's v1 limits so bad values fail here by name.
const (
	deviationMaxCandidates        = 8
	deviationRankedResultsDefault = 5
	deviationRankedResultsMax     = 10
	deviationReturnedResultsMax   = 5
	deviationMinimumCohortDefault = 100
	deviationMinimumCohortMin     = 20
	deviationValueSupportDefault  = 20
	deviationValueSupportMin      = 10
	deviationLookbackMaxMinutes   = 15
	deviationMaxResponseBodyBytes = 4 << 20
)

type GetTraceAttributeDeviationsArgs struct {
	ComparisonMode      string                   `json:"comparison_mode" jsonschema:"(Required) Cohort comparison: latency, errors, or time"`
	ServiceName         string                   `json:"service_name" jsonschema:"(Required) Exact service name to analyze"`
	Environment         string                   `json:"environment" jsonschema:"(Required) Exact deployment.environment value"`
	Operation           string                   `json:"operation,omitempty" jsonschema:"Optional exact span/operation name"`
	Population          string                   `json:"population,omitempty" jsonschema:"Population scope: service when operation is absent, operation when operation is present; the default is derived from operation"`
	Filters             []map[string]interface{} `json:"filters,omitempty" jsonschema:"Optional additional trace filter conditions in trace JSON operator form"`
	CandidateAttributes []string                 `json:"candidate_attributes,omitempty" jsonschema:"Candidate attributes from get_trace_attributes_for_pipeline; maximum 8. Omit for bounded safe discovery."`
	LatencyThresholdMs  *float64                 `json:"latency_threshold_ms,omitempty" jsonschema:"Latency split in milliseconds; use exactly one latency selector in latency mode"`
	LatencyPercentile   *float64                 `json:"latency_percentile,omitempty" jsonschema:"Latency percentile in the same scoped population; greater than 0 and less than 100; use exactly one latency selector in latency mode"`
	StartTimeISO        string                   `json:"start_time_iso,omitempty" jsonschema:"Current/analysis window start in RFC3339"`
	EndTimeISO          string                   `json:"end_time_iso,omitempty" jsonschema:"Current/analysis window end in RFC3339"`
	LookbackMinutes     int                      `json:"lookback_minutes,omitempty" jsonschema:"Lookback ending now; default 15, maximum 15"`
	BaselineStartISO    string                   `json:"baseline_start_time_iso,omitempty" jsonschema:"(Required for time mode) Equal-duration baseline start in RFC3339"`
	BaselineEndISO      string                   `json:"baseline_end_time_iso,omitempty" jsonschema:"(Required for time mode) Equal-duration baseline end in RFC3339"`
	MinimumCohortSize   int                      `json:"minimum_cohort_size,omitempty" jsonschema:"Minimum spans required in each cohort; default 100, minimum 20"`
	MinimumValueSupport int                      `json:"minimum_value_support,omitempty" jsonschema:"Minimum pooled observations for a ranked value; default 20, minimum 10"`
	Limit               int                      `json:"limit,omitempty" jsonschema:"Requested ranked deviations; default 5. Values 6-10 are accepted for legacy compatibility, but at most 5 are returned with truncation metadata."`
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
	Population  string                   `json:"population"`
	Filters     []map[string]interface{} `json:"filters,omitempty"`
}

type deviationAPIComparison struct {
	Mode               string             `json:"mode"`
	Target             deviationAPIWindow `json:"target"`
	Control            deviationAPIWindow `json:"control"`
	LatencyThresholdMs *float64           `json:"latency_threshold_ms,omitempty"`
	LatencyPercentile  *float64           `json:"latency_percentile,omitempty"`
}

type deviationAPIWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type deviationAPICandidates struct {
	Attributes   []string `json:"attributes"`
	AutoDiscover bool     `json:"auto_discover"`
}

// Candidate, per-attribute value, and representative budgets are omitted so the
// endpoint applies its own defaults.
type deviationAPILimits struct {
	MinimumCohortSize    int `json:"minimum_cohort_size"`
	MinimumValueSupport  int `json:"minimum_value_support"`
	MaximumRankedResults int `json:"maximum_ranked_results"`
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
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(body)}},
			StructuredContent: json.RawMessage(body),
		}, nil, nil
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
	population, err := deviationPopulation(args)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	target, err := deviationTargetWindow(args, now)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	control, err := deviationControlWindow(args, mode, target)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	threshold, percentile, err := deviationLatencySelectors(mode, args.LatencyThresholdMs, args.LatencyPercentile)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	limits, err := deviationLimits(args)
	if err != nil {
		return deviationAPIRequest{}, err
	}
	return newDeviationAPIRequest(args, population, mode, target, control, threshold, percentile, limits), nil
}

func deviationPopulation(args GetTraceAttributeDeviationsArgs) (string, error) {
	population := strings.ToLower(strings.TrimSpace(args.Population))
	operationPresent := strings.TrimSpace(args.Operation) != ""
	if population == "" {
		if operationPresent {
			return "operation", nil
		}
		return "service", nil
	}
	switch population {
	case "service":
		if operationPresent {
			return "", fmt.Errorf("operation must be omitted when population is service")
		}
	case "operation":
		if !operationPresent {
			return "", fmt.Errorf("operation is required when population is operation")
		}
	default:
		return "", fmt.Errorf("population must be service or operation")
	}
	return population, nil
}

// deviationLimits defaults zero values and rejects out-of-range ones instead of clamping.
func deviationLimits(args GetTraceAttributeDeviationsArgs) (deviationAPILimits, error) {
	if len(args.CandidateAttributes) > deviationMaxCandidates {
		return deviationAPILimits{}, fmt.Errorf("candidate_attributes accepts at most %d attributes, got %d", deviationMaxCandidates, len(args.CandidateAttributes))
	}
	cohort := args.MinimumCohortSize
	if cohort == 0 {
		cohort = deviationMinimumCohortDefault
	} else if cohort < deviationMinimumCohortMin {
		return deviationAPILimits{}, fmt.Errorf("minimum_cohort_size must be at least %d", deviationMinimumCohortMin)
	}
	support := args.MinimumValueSupport
	if support == 0 {
		support = deviationValueSupportDefault
	} else if support < deviationValueSupportMin {
		return deviationAPILimits{}, fmt.Errorf("minimum_value_support must be at least %d", deviationValueSupportMin)
	}
	limit := args.Limit
	if limit == 0 {
		limit = deviationRankedResultsDefault
	} else if limit < 1 || limit > deviationRankedResultsMax {
		return deviationAPILimits{}, fmt.Errorf("limit must be between 1 and %d", deviationRankedResultsMax)
	}
	return deviationAPILimits{
		MinimumCohortSize:    cohort,
		MinimumValueSupport:  support,
		MaximumRankedResults: limit,
	}, nil
}

func deviationTargetWindow(args GetTraceAttributeDeviationsArgs, now time.Time) (deviationAPIWindow, error) {
	params := map[string]interface{}{}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	if args.LookbackMinutes != 0 {
		if args.LookbackMinutes < 0 || args.LookbackMinutes > deviationLookbackMaxMinutes {
			return deviationAPIWindow{}, fmt.Errorf("lookback_minutes must be between 1 and %d, got %d", deviationLookbackMaxMinutes, args.LookbackMinutes)
		}
		params["lookback_minutes"] = args.LookbackMinutes
	}
	start, end, err := utils.GetTimeRangeAt(params, deviationLookbackMaxMinutes, now)
	if err != nil {
		return deviationAPIWindow{}, err
	}
	if end.Sub(start) > maxDeviationWindow {
		return deviationAPIWindow{}, fmt.Errorf("the effective window from start_time_iso/end_time_iso must not exceed %d minutes, got %s", deviationLookbackMaxMinutes, end.Sub(start))
	}
	return deviationAPIWindow{Start: start, End: end}, nil
}

func deviationControlWindow(args GetTraceAttributeDeviationsArgs, mode string, target deviationAPIWindow) (deviationAPIWindow, error) {
	if mode != "time" {
		return target, nil
	}
	// Same parser as the target window: a format valid in start_time_iso must not
	// be rejected in its baseline sibling.
	start, err := utils.ParseToolTimestamp(args.BaselineStartISO)
	if err != nil {
		return deviationAPIWindow{}, fmt.Errorf("baseline_start_time_iso %q is not a supported timestamp: %w", args.BaselineStartISO, err)
	}
	end, err := utils.ParseToolTimestamp(args.BaselineEndISO)
	if err != nil {
		return deviationAPIWindow{}, fmt.Errorf("baseline_end_time_iso %q is not a supported timestamp: %w", args.BaselineEndISO, err)
	}
	if !end.After(start) {
		return deviationAPIWindow{}, fmt.Errorf("baseline_end_time_iso %q must be after baseline_start_time_iso %q", args.BaselineEndISO, args.BaselineStartISO)
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

func deviationLatencySelectors(mode string, thresholdValue, percentileValue *float64) (*float64, *float64, error) {
	if mode == "latency" {
		if (thresholdValue != nil) == (percentileValue != nil) {
			return nil, nil, fmt.Errorf("latency mode requires exactly one of latency_threshold_ms or latency_percentile")
		}
		if thresholdValue != nil && *thresholdValue <= 0 {
			return nil, nil, fmt.Errorf("latency_threshold_ms must be positive for latency mode")
		}
		if percentileValue != nil && (*percentileValue <= 0 || *percentileValue >= 100) {
			return nil, nil, fmt.Errorf("latency_percentile must be greater than 0 and less than 100")
		}
		if thresholdValue != nil {
			return thresholdValue, nil, nil
		}
		return nil, percentileValue, nil
	}
	if thresholdValue != nil || percentileValue != nil {
		return nil, nil, fmt.Errorf("latency threshold selectors are only valid for latency mode")
	}
	return nil, nil, nil
}

func newDeviationAPIRequest(args GetTraceAttributeDeviationsArgs, population, mode string, target, control deviationAPIWindow, threshold, percentile *float64, limits deviationAPILimits) deviationAPIRequest {
	return deviationAPIRequest{
		ContractVersion: attributeDeviationsVersion,
		Scope:           deviationAPIScope{ServiceName: args.ServiceName, Environment: args.Environment, Operation: args.Operation, Population: population, Filters: args.Filters},
		Comparison:      deviationAPIComparison{Mode: mode, Target: target, Control: control, LatencyThresholdMs: threshold, LatencyPercentile: percentile},
		Candidates:      deviationAPICandidates{Attributes: args.CandidateAttributes, AutoDiscover: len(args.CandidateAttributes) == 0},
		Limits:          limits,
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
		return nil, newTraceTransportError(err)
	}
	defer response.Body.Close()
	// Status before body: newTraceHTTPError echoes only 400/422 (sanitized), drains 5xx.
	if response.StatusCode != http.StatusOK {
		return nil, newTraceHTTPError(response)
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, deviationMaxResponseBodyBytes+1))
	if readErr != nil {
		return nil, newTraceInvalidResponseError(readErr)
	}
	if len(responseBody) > deviationMaxResponseBodyBytes {
		return nil, newTraceInvalidResponseError(fmt.Errorf("attribute-deviations response exceeds %d bytes", deviationMaxResponseBodyBytes))
	}
	if !json.Valid(responseBody) {
		return nil, newTraceInvalidResponseError(nil)
	}
	if err := checkDeviationResponseContract(responseBody); err != nil {
		return nil, err
	}
	return responseBody, nil
}

// The handler forwards this body verbatim, so a payload that is not the promised
// envelope would have the model reasoning over fields that do not exist.
func checkDeviationResponseContract(body []byte) error {
	var envelope struct {
		ContractVersion string `json:"contract_version"`
		AnalysisVersion string `json:"analysis_version"`
		Request         *struct {
			Scope           json.RawMessage `json:"scope"`
			RequestedWindow json.RawMessage `json:"requested_window"`
			EffectiveWindow json.RawMessage `json:"effective_window"`
		} `json:"request"`
		Evidence *struct {
			Partial    *bool              `json:"partial"`
			Truncated  *bool              `json:"truncated"`
			Warnings   *[]json.RawMessage `json:"warnings"`
			Provenance json.RawMessage    `json:"provenance"`
		} `json:"evidence"`
		Interpretation *struct {
			EvidenceQuality *string            `json:"evidence_quality"`
			ClaimType       *string            `json:"claim_type"`
			Summary         *string            `json:"summary"`
			Limitations     *[]json.RawMessage `json:"limitations"`
		} `json:"interpretation"`
		Data *struct {
			Deviations *[]json.RawMessage `json:"deviations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return newTraceInvalidResponseError(err)
	}
	if envelope.ContractVersion != investigationEvidenceVersion {
		return newTraceInvalidResponseError(fmt.Errorf(
			"expected contract_version %q, got %q", investigationEvidenceVersion, envelope.ContractVersion))
	}
	if envelope.AnalysisVersion != attributeDeviationsVersion {
		return newTraceInvalidResponseError(fmt.Errorf(
			"expected analysis_version %q, got %q", attributeDeviationsVersion, envelope.AnalysisVersion))
	}
	if envelope.Request == nil || !rawJSONObject(envelope.Request.Scope) || !rawJSONObject(envelope.Request.RequestedWindow) || !rawJSONObject(envelope.Request.EffectiveWindow) {
		return newTraceInvalidResponseError(fmt.Errorf("response request must contain object scope and windows"))
	}
	if envelope.Evidence == nil || envelope.Evidence.Partial == nil || envelope.Evidence.Truncated == nil || envelope.Evidence.Warnings == nil || !rawJSONObject(envelope.Evidence.Provenance) {
		return newTraceInvalidResponseError(fmt.Errorf("response evidence is missing required typed fields"))
	}
	if envelope.Interpretation == nil || envelope.Interpretation.EvidenceQuality == nil || envelope.Interpretation.ClaimType == nil || envelope.Interpretation.Summary == nil || envelope.Interpretation.Limitations == nil {
		return newTraceInvalidResponseError(fmt.Errorf("response interpretation is missing required typed fields"))
	}
	if envelope.Data == nil || envelope.Data.Deviations == nil {
		return newTraceInvalidResponseError(fmt.Errorf("response missing required data.deviations array"))
	}
	if len(*envelope.Data.Deviations) > deviationReturnedResultsMax {
		return newTraceInvalidResponseError(fmt.Errorf(
			"response returned %d deviations; maximum supported is %d", len(*envelope.Data.Deviations), deviationReturnedResultsMax))
	}
	return nil
}

func rawJSONObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &object) == nil && object != nil
}
