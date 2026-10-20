package traces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	apitraces "last9-mcp/internal/last9api/traces"
	"last9-mcp/internal/models"
	"last9-mcp/internal/otelids"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	traceWaterfallAnalysisVersion = "trace-waterfall/v1"
	traceEvidenceSanitizerVersion = "trace-evidence-sanitizer/v1"
	traceWaterfallMaxSpansDefault = 500
	traceWaterfallMaxSpansCeiling = 1000
)

type GetTraceWaterfallArgs struct {
	TraceID         string `json:"trace_id" jsonschema:"(Required) Exact 32-character hexadecimal OpenTelemetry trace ID."`
	Environment     string `json:"environment,omitempty" jsonschema:"Optional exact deployment.environment value"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Lookback ending now; default 4320 minutes"`
	SelectedSpanID  string `json:"selected_span_id,omitempty" jsonschema:"Optional 16-character hexadecimal OpenTelemetry span ID whose attributes, events, and links should be returned."`
	MaxSpans        int    `json:"max_spans,omitempty" jsonschema:"Maximum spans; default 500, maximum 1000"`
}

// NewGetTraceWaterfallHandler relays the trace service's sanitized waterfall
// evidence verbatim; building and redaction happen server-side.
func NewGetTraceWaterfallHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
		in, err := waterfallInput(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := apitraces.GetWaterfall(ctx, last9api.NewClient(client, cfg), in)
		if err != nil {
			return nil, nil, waterfallAPIError(err)
		}
		if err := checkWaterfallEvidence(body); err != nil {
			return nil, nil, err
		}
		dl := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildTracesLink(in.Start*1000, in.End*1000, nil, in.TraceID, "")
		return &mcp.CallToolResult{Meta: deeplink.ToMeta(dl), Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

func waterfallInput(args GetTraceWaterfallArgs) (apitraces.GetWaterfallInput, error) {
	traceID, err := otelids.NormalizeTraceID(args.TraceID)
	if err != nil {
		return apitraces.GetWaterfallInput{}, rejectOTelID("get_trace_waterfall", err)
	}
	spanID := ""
	if args.SelectedSpanID != "" {
		if spanID, err = otelids.NormalizeSpanID(args.SelectedSpanID); err != nil {
			return apitraces.GetWaterfallInput{}, rejectOTelID("get_trace_waterfall", err)
		}
	}
	maxSpans := args.MaxSpans
	if maxSpans == 0 {
		maxSpans = traceWaterfallMaxSpansDefault
	}
	if maxSpans < 1 || maxSpans > traceWaterfallMaxSpansCeiling {
		return apitraces.GetWaterfallInput{}, fmt.Errorf("max_spans must be between 1 and %d", traceWaterfallMaxSpansCeiling)
	}
	lookback := args.LookbackMinutes
	if lookback == 0 {
		lookback = TraceIDLookbackMinutesDefault
	}
	params := map[string]interface{}{}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	start, end, err := utils.GetTimeRange(params, lookback)
	if err != nil {
		return apitraces.GetWaterfallInput{}, err
	}
	return apitraces.GetWaterfallInput{
		TraceID:        traceID,
		Start:          start.Unix(),
		End:            end.Unix(),
		MaxSpans:       maxSpans,
		SelectedSpanID: spanID,
		Environment:    args.Environment,
	}, nil
}

// A *url.Error carries the request URL, so transport failures keep the trace
// tools' generic message.
func waterfallAPIError(err error) error {
	var tooLarge *last9api.ResponseTooLargeError
	if errors.As(err, &tooLarge) {
		return &waterfallRefusedError{Reason: refusalTooLarge}
	}
	var transport *url.Error
	if errors.As(err, &transport) {
		return newTraceTransportError(err)
	}
	return err
}

// checkWaterfallEvidence fails closed unless the body is waterfall evidence that
// names the sanitizer it went through. Keys are read exactly: encoding/json would
// otherwise accept "Sanitizer", which a client reading "sanitizer" never sees.
func checkWaterfallEvidence(body json.RawMessage) error {
	if err := checkEvidenceContract(body, traceWaterfallAnalysisVersion); err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return newTraceInvalidResponseError(err)
	}
	var sanitizer map[string]json.RawMessage
	if err := json.Unmarshal(top["sanitizer"], &sanitizer); err != nil || sanitizer == nil {
		return &waterfallRefusedError{Reason: refusalSanitizerMissing}
	}
	var version string
	if err := json.Unmarshal(sanitizer["version"], &version); err != nil || version != traceEvidenceSanitizerVersion {
		return &waterfallRefusedError{Reason: refusalSanitizerVersion}
	}
	return nil
}

type waterfallRefusal string

const (
	refusalSanitizerMissing waterfallRefusal = "sanitizer_missing"
	refusalSanitizerVersion waterfallRefusal = "sanitizer_version_unsupported"
	refusalTooLarge         waterfallRefusal = "evidence_too_large"
)

// waterfallRefusedError is evidence get_trace_waterfall will not relay.
type waterfallRefusedError struct{ Reason waterfallRefusal }

func (e *waterfallRefusedError) Error() string {
	switch e.Reason {
	case refusalTooLarge:
		return fmt.Sprintf("The trace waterfall exceeds the %d-byte output cap. Lower max_spans, or omit selected_span_id.", apitraces.MaxWaterfallBytes)
	case refusalSanitizerVersion:
		return fmt.Sprintf("The trace service's waterfall was not redacted by %s, so it was withheld. Retrying will not help.", traceEvidenceSanitizerVersion)
	default:
		return "The trace service's waterfall carries no sanitizer block, so it may be unredacted and was withheld. Retrying will not help."
	}
}
