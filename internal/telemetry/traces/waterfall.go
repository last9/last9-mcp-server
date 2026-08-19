package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// Only stamp this once the payload satisfies contracts/investigation-evidence-v1.schema.json.
	investigationEvidenceVersion  = "investigation-evidence/v1"
	traceWaterfallAnalysisVersion = "trace-waterfall/v1"
	traceWaterfallSource          = "trace-details"
	// Half-open [start,end), per contracts/README.md.
	evidenceWindowBoundary           = "half-open"
	traceWaterfallMaxSpansDefault    = 500
	traceWaterfallMaxSpansCeiling    = 1000
	traceWaterfallMaxResponseBytes   = 16 << 20
	traceWaterfallTopN               = 5
	traceWaterfallErrorStatusCode    = "STATUS_CODE_ERROR"
	traceWaterfallClaimObservation   = "observation"
	traceWaterfallClaimContribution  = "contribution"
	evidenceQualityHigh              = "high"
	evidenceQualityMedium            = "medium"
	evidenceQualityInsufficient      = "insufficient"
	traceWaterfallTruncationWarning  = "result reached max_spans; child spans may be missing and self_time_ms may be overstated"
	traceWaterfallEmptyResultWarning = "no spans found for this trace_id in the requested window"
	// Trailing space: the offending span ID is appended.
	traceWaterfallSelectedSpanMissingWarning = "selected_span_id not found in returned spans: "
)

type GetTraceWaterfallArgs struct {
	TraceID         string `json:"trace_id" jsonschema:"(Required) Exact trace ID"`
	Environment     string `json:"environment,omitempty" jsonschema:"Optional exact deployment.environment value"`
	StartTimeISO    string `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339"`
	EndTimeISO      string `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339"`
	LookbackMinutes int    `json:"lookback_minutes,omitempty" jsonschema:"Lookback ending now; default 4320 minutes"`
	SelectedSpanID  string `json:"selected_span_id,omitempty" jsonschema:"Optional span ID whose attributes, events, and links should be returned"`
	MaxSpans        int    `json:"max_spans,omitempty" jsonschema:"Maximum spans; default 500, maximum 1000"`
}

type WaterfallSpan struct {
	SpanID        string  `json:"span_id"`
	ParentSpanID  string  `json:"parent_span_id,omitempty"`
	Service       string  `json:"service"`
	Operation     string  `json:"operation"`
	Kind          string  `json:"kind"`
	Status        string  `json:"status"`
	StartOffsetMs float64 `json:"start_offset_ms"`
	DurationMs    float64 `json:"duration_ms"`
	SelfTimeMs    float64 `json:"self_time_ms"`
	Depth         int     `json:"depth"`
}

type WaterfallSelectedSpan struct {
	SpanID             string                   `json:"span_id"`
	ResourceAttributes map[string]string        `json:"resource_attributes,omitempty"`
	SpanAttributes     map[string]interface{}   `json:"span_attributes,omitempty"`
	Events             []map[string]interface{} `json:"events,omitempty"`
	Links              []map[string]interface{} `json:"links,omitempty"`
}

// EvidenceWindow is a half-open [start,end) window.
type EvidenceWindow struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Boundary string `json:"boundary"`
}

// EvidenceProvenance records the evidence source and observation time.
type EvidenceProvenance struct {
	Source     string  `json:"source"`
	ObservedAt string  `json:"observed_at"`
	QueryID    *string `json:"query_id"`
}

// WaterfallScope identifies a trace-scoped analysis. The evidence envelope also
// allows a service_name + environment scope, which cohort analyses use instead.
type WaterfallScope struct {
	TraceID     string `json:"trace_id"`
	Environment string `json:"environment,omitempty"`
}

type WaterfallRequest struct {
	Scope           WaterfallScope `json:"scope"`
	RequestedWindow EvidenceWindow `json:"requested_window"`
	EffectiveWindow EvidenceWindow `json:"effective_window"`
	TraceID         string         `json:"trace_id"`
	MaxSpans        int            `json:"max_spans"`
	SelectedSpanID  string         `json:"selected_span_id,omitempty"`
}

type WaterfallEvidence struct {
	Partial       bool                 `json:"partial"`
	Truncated     bool                 `json:"truncated"`
	Warnings      []string             `json:"warnings"`
	ReturnedSpans int                  `json:"returned_spans"`
	AppliedLimit  int                  `json:"applied_limit"`
	Provenance    EvidenceProvenance   `json:"provenance"`
	Sanitization  EvidenceSanitization `json:"sanitization"`
}

type WaterfallInterpretation struct {
	EvidenceQuality string   `json:"evidence_quality"`
	ClaimType       string   `json:"claim_type"`
	Summary         string   `json:"summary"`
	Limitations     []string `json:"limitations"`
}

// Start/End are omitted when no span had parseable bounds: a consumer parsing them
// as date-time breaks on "" but not on an absent key.
type WaterfallSummary struct {
	Start        string   `json:"start,omitempty"`
	End          string   `json:"end,omitempty"`
	DurationMs   float64  `json:"duration_ms"`
	SpanCount    int      `json:"span_count"`
	ServiceCount int      `json:"service_count"`
	ErrorCount   int      `json:"error_count"`
	MaxDepth     int      `json:"max_depth"`
	RootSpanIDs  []string `json:"root_span_ids"`
}

type WaterfallData struct {
	Summary                     WaterfallSummary       `json:"summary"`
	Spans                       []WaterfallSpan        `json:"spans"`
	SlowestSpans                []WaterfallSpan        `json:"slowest_spans"`
	LargestSelfTimeContributors []WaterfallSpan        `json:"largest_self_time_contributors"`
	SelectedSpan                *WaterfallSelectedSpan `json:"selected_span,omitempty"`
}

// TraceWaterfallResponse is an investigation-evidence/v1 envelope; keep the contract test green.
type TraceWaterfallResponse struct {
	ContractVersion string                  `json:"contract_version"`
	AnalysisVersion string                  `json:"analysis_version"`
	Request         WaterfallRequest        `json:"request"`
	Evidence        WaterfallEvidence       `json:"evidence"`
	Interpretation  WaterfallInterpretation `json:"interpretation"`
	Data            WaterfallData           `json:"data"`
}

func NewGetTraceWaterfallHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
		if args.TraceID == "" {
			return nil, nil, fmt.Errorf("trace_id is required")
		}
		maxSpans := args.MaxSpans
		if maxSpans == 0 {
			maxSpans = traceWaterfallMaxSpansDefault
		}
		if maxSpans < 1 || maxSpans > traceWaterfallMaxSpansCeiling {
			return nil, nil, fmt.Errorf("max_spans must be between 1 and %d", traceWaterfallMaxSpansCeiling)
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
			return nil, nil, err
		}
		// No Env: the trace-details URL has no environment parameter, so filtering
		// happens client-side below via traceDetailsMatchesEnv.
		qp := &GetTracesQueryParams{TraceID: args.TraceID, Region: cfg.Region, Limit: maxSpans}
		u, err := buildTraceDetailsRequestURL(cfg, qp, start.Unix(), end.Unix())
		if err != nil {
			return nil, nil, err
		}
		req, err := createTraceDetailsRequest(ctx, u, cfg)
		if err != nil {
			return nil, nil, err
		}
		httpResp, err := client.Do(req)
		if err != nil {
			return nil, nil, newTraceTransportError(err)
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode == http.StatusNotFound {
			return nil, nil, newTraceNotFoundTraceError()
		}
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, newTraceHTTPError(httpResp)
		}
		responseBody, err := io.ReadAll(io.LimitReader(httpResp.Body, traceWaterfallMaxResponseBytes+1))
		if err != nil {
			return nil, nil, newTraceInvalidResponseError(err)
		}
		if len(responseBody) > traceWaterfallMaxResponseBytes {
			return nil, nil, newTraceInvalidResponseError(fmt.Errorf("trace-details response exceeds %d bytes", traceWaterfallMaxResponseBytes))
		}
		var raw TraceDetailsResponse
		if err := json.Unmarshal(responseBody, &raw); err != nil {
			return nil, nil, newTraceInvalidResponseError(err)
		}
		filtered := make([]TraceDetailsSpan, 0, len(raw.Traces))
		for _, s := range raw.Traces {
			if traceDetailsMatchesEnv(s, args.Environment) {
				filtered = append(filtered, s)
			}
		}
		resp := buildTraceWaterfall(waterfallBuildInput{
			traceID:      args.TraceID,
			environment:  args.Environment,
			spans:        filtered,
			fetchedSpans: len(raw.Traces),
			limit:        maxSpans,
			selectedID:   args.SelectedSpanID,
			// Equal because this path never clamps; the envelope requires both fields.
			requested:  evidenceWindow(start, end),
			effective:  evidenceWindow(start, end),
			observedAt: time.Now().UTC(),
		})
		b, err := marshalSanitizedTraceWaterfall(resp)
		if err != nil {
			return nil, nil, err
		}
		dl := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildTracesLink(start.UnixMilli(), end.UnixMilli(), nil, args.TraceID, "")
		return newTraceWaterfallToolResult(b, deeplink.ToMeta(dl)), nil, nil
	}
}

type spanInterval struct{ start, end int64 }

// fetchedSpans is the pre-env-filter count: the set the API applied the limit to.
type waterfallBuildInput struct {
	traceID      string
	environment  string
	spans        []TraceDetailsSpan
	fetchedSpans int
	limit        int
	selectedID   string
	requested    EvidenceWindow
	effective    EvidenceWindow
	observedAt   time.Time
}

func evidenceWindow(start, end time.Time) EvidenceWindow {
	return EvidenceWindow{
		Start:    start.UTC().Format(time.RFC3339Nano),
		End:      end.UTC().Format(time.RFC3339Nano),
		Boundary: evidenceWindowBoundary,
	}
}

func buildTraceWaterfall(in waterfallBuildInput) TraceWaterfallResponse {
	var resp TraceWaterfallResponse
	resp.ContractVersion = investigationEvidenceVersion
	resp.AnalysisVersion = traceWaterfallAnalysisVersion
	resp.Request = WaterfallRequest{
		Scope:           WaterfallScope{TraceID: in.traceID, Environment: in.environment},
		RequestedWindow: in.requested,
		EffectiveWindow: in.effective,
		TraceID:         in.traceID,
		MaxSpans:        in.limit,
		SelectedSpanID:  in.selectedID,
	}
	resp.Evidence.AppliedLimit = in.limit
	resp.Evidence.Provenance = EvidenceProvenance{
		Source:     traceWaterfallSource,
		ObservedAt: in.observedAt.UTC().Format(time.RFC3339Nano),
	}

	warnings := []string{}
	byID := map[string]TraceDetailsSpan{}
	startNano := map[string]int64{}
	for _, s := range in.spans {
		if s.SpanID == "" {
			warnings = append(warnings, "span with empty span ID excluded")
			continue
		}
		if _, ok := byID[s.SpanID]; ok {
			warnings = append(warnings, "duplicate span ID: "+s.SpanID)
			continue
		}
		// Excluding beats deriving a bogus offset and self time from instant zero.
		st, ok := parseTraceTimestampNano(s.Timestamp)
		if !ok {
			warnings = append(warnings, "span with unparseable timestamp excluded: "+s.SpanID)
			continue
		}
		byID[s.SpanID] = s
		startNano[s.SpanID] = st
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	children := map[string][]string{}
	roots := []string{}
	for _, id := range ids {
		s := byID[id]
		if s.ParentSpanID == "" {
			roots = append(roots, id)
		} else if _, ok := byID[s.ParentSpanID]; !ok {
			roots = append(roots, id)
			warnings = append(warnings, "missing parent for span: "+id)
		} else {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], id)
		}
	}
	sort.Strings(roots)

	// Tracked explicitly: instant zero is legal, so it cannot mean "unset".
	haveBounds := false
	var minStart, maxEnd int64
	for _, id := range ids {
		st := startNano[id]
		en := st + byID[id].Duration
		if !haveBounds {
			minStart, maxEnd, haveBounds = st, en, true
			continue
		}
		if st < minStart {
			minStart = st
		}
		if en > maxEnd {
			maxEnd = en
		}
	}

	depth := map[string]int{}
	visiting := map[string]bool{}
	var visit func(string, int)
	visit = func(id string, d int) {
		if visiting[id] {
			warnings = append(warnings, "cycle detected at span: "+id)
			return
		}
		if old, ok := depth[id]; ok && old <= d {
			return
		}
		visiting[id] = true
		depth[id] = d
		sort.Strings(children[id])
		for _, c := range children[id] {
			visit(c, d+1)
		}
		visiting[id] = false
	}
	for _, r := range roots {
		visit(r, 0)
	}
	remaining := make([]string, 0)
	for _, id := range ids {
		if _, ok := depth[id]; !ok {
			remaining = append(remaining, id)
		}
	}
	for _, id := range remaining {
		if _, ok := depth[id]; ok {
			continue
		}
		warnings = append(warnings, "disconnected graph component at span: "+id)
		visit(id, 0)
	}

	services := map[string]bool{}
	for _, id := range ids {
		s := byID[id]
		st := startNano[id]
		own := spanInterval{st, st + s.Duration}
		intervals := []spanInterval{}
		for _, cid := range children[id] {
			c := byID[cid]
			cs := startNano[cid]
			ce := cs + c.Duration
			if cs < own.start || ce > own.end {
				warnings = append(warnings, "child interval outside parent: "+cid)
			}
			if cs < own.start {
				cs = own.start
			}
			if ce > own.end {
				ce = own.end
			}
			if ce > cs {
				intervals = append(intervals, spanInterval{cs, ce})
			}
		}
		self := s.Duration - unionDuration(intervals)
		if self < 0 {
			self = 0
		}
		w := WaterfallSpan{SpanID: id, ParentSpanID: s.ParentSpanID, Service: s.ServiceName, Operation: s.SpanName, Kind: s.SpanKind, Status: s.StatusCode, StartOffsetMs: float64(st-minStart) / 1e6, DurationMs: float64(s.Duration) / 1e6, SelfTimeMs: float64(self) / 1e6, Depth: depth[id]}
		resp.Data.Spans = append(resp.Data.Spans, w)
		if s.ServiceName != "" {
			services[s.ServiceName] = true
		}
		if s.StatusCode == traceWaterfallErrorStatusCode {
			resp.Data.Summary.ErrorCount++
		}
		if depth[id] > resp.Data.Summary.MaxDepth {
			resp.Data.Summary.MaxDepth = depth[id]
		}
		if id == in.selectedID {
			resp.Data.SelectedSpan = &WaterfallSelectedSpan{SpanID: id, ResourceAttributes: s.ResourceAttributes, SpanAttributes: s.SpanAttributes, Events: s.Events, Links: s.Links}
		}
	}
	sort.Slice(resp.Data.Spans, func(i, j int) bool {
		if resp.Data.Spans[i].StartOffsetMs != resp.Data.Spans[j].StartOffsetMs {
			return resp.Data.Spans[i].StartOffsetMs < resp.Data.Spans[j].StartOffsetMs
		}
		return resp.Data.Spans[i].SpanID < resp.Data.Spans[j].SpanID
	})
	if resp.Data.Spans == nil {
		resp.Data.Spans = []WaterfallSpan{}
	}
	resp.Data.SlowestSpans = topWaterfall(resp.Data.Spans, func(s WaterfallSpan) float64 { return s.DurationMs })
	resp.Data.LargestSelfTimeContributors = topWaterfall(resp.Data.Spans, func(s WaterfallSpan) float64 { return s.SelfTimeMs })
	resp.Data.Summary.SpanCount = len(resp.Data.Spans)
	resp.Data.Summary.ServiceCount = len(services)
	resp.Data.Summary.RootSpanIDs = roots
	if resp.Data.Summary.RootSpanIDs == nil {
		resp.Data.Summary.RootSpanIDs = []string{}
	}
	if haveBounds {
		resp.Data.Summary.DurationMs = float64(maxEnd-minStart) / 1e6
		resp.Data.Summary.Start = time.Unix(0, minStart).UTC().Format(time.RFC3339Nano)
		resp.Data.Summary.End = time.Unix(0, maxEnd).UTC().Format(time.RFC3339Nano)
	}

	// Judged on the fetched count: the API applied max_spans before env filtering.
	resp.Evidence.Truncated = in.limit > 0 && in.fetchedSpans >= in.limit
	if resp.Evidence.Truncated {
		warnings = append(warnings, traceWaterfallTruncationWarning)
	}
	resp.Evidence.ReturnedSpans = len(resp.Data.Spans)
	// Spans dropped while building are partial; ones the caller's env filter removed are not.
	resp.Evidence.Partial = resp.Evidence.Truncated || len(byID) < len(in.spans)

	// Otherwise a wrong ID and a span with no attributes look identical: both
	// simply omit selected_span.
	if in.selectedID != "" && resp.Data.SelectedSpan == nil {
		warnings = append(warnings, traceWaterfallSelectedSpanMissingWarning+in.selectedID)
	}

	resp.Interpretation.ClaimType = traceWaterfallClaimContribution
	switch {
	case len(resp.Data.Spans) == 0:
		warnings = append(warnings, traceWaterfallEmptyResultWarning)
		resp.Interpretation.EvidenceQuality = evidenceQualityInsufficient
		resp.Interpretation.ClaimType = traceWaterfallClaimObservation
	case resp.Evidence.Truncated || len(warnings) > 0:
		resp.Interpretation.EvidenceQuality = evidenceQualityMedium
	default:
		resp.Interpretation.EvidenceQuality = evidenceQualityHigh
	}

	// Sorted so identical input yields identical bytes; contracts/README.md hashes evidence.
	sort.Strings(warnings)
	resp.Evidence.Warnings = warnings

	resp.Interpretation.Summary = "Compact trace waterfall with interval-union self-time. Slowest spans and self-time contributors are observations, not proof of cause."
	resp.Interpretation.Limitations = []string{"V1 does not compute or claim a critical path."}
	if resp.Evidence.Truncated {
		resp.Interpretation.Limitations = append(resp.Interpretation.Limitations, "Truncated result: a parent's self_time_ms may absorb the duration of children that were not returned.")
	}
	if len(resp.Data.Spans) == 0 {
		resp.Interpretation.Limitations = append(resp.Interpretation.Limitations, "An empty waterfall is not evidence that the trace does not exist; widen the window or verify the trace ID.")
	}
	return resp
}

func unionDuration(xs []spanInterval) int64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].start < xs[j].start })
	total := int64(0)
	cur := xs[0]
	for _, x := range xs[1:] {
		if x.start <= cur.end {
			if x.end > cur.end {
				cur.end = x.end
			}
		} else {
			total += cur.end - cur.start
			cur = x
		}
	}
	return total + cur.end - cur.start
}

func topWaterfall(all []WaterfallSpan, value func(WaterfallSpan) float64) []WaterfallSpan {
	out := append([]WaterfallSpan{}, all...)
	sort.Slice(out, func(i, j int) bool {
		a, b := value(out[i]), value(out[j])
		if a != b {
			return a > b
		}
		return out[i].SpanID < out[j].SpanID
	})
	if len(out) > traceWaterfallTopN {
		out = out[:traceWaterfallTopN]
	}
	return out
}
