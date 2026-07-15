package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

type TraceWaterfallResponse struct {
	ContractVersion string `json:"contract_version"`
	TraceID         string `json:"trace_id"`
	Summary         struct {
		Start        string   `json:"start"`
		End          string   `json:"end"`
		DurationMs   float64  `json:"duration_ms"`
		SpanCount    int      `json:"span_count"`
		ServiceCount int      `json:"service_count"`
		ErrorCount   int      `json:"error_count"`
		MaxDepth     int      `json:"max_depth"`
		RootSpanIDs  []string `json:"root_span_ids"`
	} `json:"summary"`
	Evidence struct {
		ReturnedSpans int      `json:"returned_spans"`
		AppliedLimit  int      `json:"applied_limit"`
		Truncated     bool     `json:"truncated"`
		Warnings      []string `json:"warnings"`
	} `json:"evidence"`
	Spans                       []WaterfallSpan        `json:"spans"`
	SlowestSpans                []WaterfallSpan        `json:"slowest_spans"`
	LargestSelfTimeContributors []WaterfallSpan        `json:"largest_self_time_contributors"`
	SelectedSpan                *WaterfallSelectedSpan `json:"selected_span,omitempty"`
	Interpretation              struct {
		EvidenceQuality string   `json:"evidence_quality"`
		Summary         string   `json:"summary"`
		Limitations     []string `json:"limitations"`
	} `json:"interpretation"`
}

func NewGetTraceWaterfallHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetTraceWaterfallArgs) (*mcp.CallToolResult, any, error) {
		if args.TraceID == "" {
			return nil, nil, fmt.Errorf("trace_id is required")
		}
		max := args.MaxSpans
		if max == 0 {
			max = 500
		}
		if max < 1 || max > 1000 {
			return nil, nil, fmt.Errorf("max_spans must be between 1 and 1000")
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
		qp := &GetTracesQueryParams{TraceID: args.TraceID, Region: cfg.Region, Limit: max, Env: args.Environment}
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
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("trace details API returned status %d", httpResp.StatusCode)
		}
		var raw TraceDetailsResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&raw); err != nil {
			return nil, nil, err
		}
		filtered := make([]TraceDetailsSpan, 0, len(raw.Traces))
		for _, s := range raw.Traces {
			if traceDetailsMatchesEnv(s, args.Environment) {
				filtered = append(filtered, s)
			}
		}
		resp := buildTraceWaterfall(args.TraceID, filtered, max, args.SelectedSpanID)
		b, err := json.Marshal(resp)
		if err != nil {
			return nil, nil, err
		}
		dl := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildTracesLink(start.UnixMilli(), end.UnixMilli(), nil, args.TraceID, "")
		return &mcp.CallToolResult{Meta: deeplink.ToMeta(dl), Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
	}
}

type spanInterval struct{ start, end int64 }

func buildTraceWaterfall(traceID string, raw []TraceDetailsSpan, limit int, selectedID string) TraceWaterfallResponse {
	var resp TraceWaterfallResponse
	resp.ContractVersion = "investigation-evidence/v1"
	resp.TraceID = traceID
	resp.Evidence.AppliedLimit = limit
	resp.Evidence.Truncated = len(raw) >= limit
	warnings := []string{}
	byID := map[string]TraceDetailsSpan{}
	for _, s := range raw {
		if s.SpanID == "" {
			warnings = append(warnings, "span with empty span ID excluded")
			continue
		}
		if _, ok := byID[s.SpanID]; ok {
			warnings = append(warnings, "duplicate span ID: "+s.SpanID)
			continue
		}
		byID[s.SpanID] = s
	}
	children := map[string][]string{}
	roots := []string{}
	for id, s := range byID {
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
	minStart, maxEnd := int64(0), int64(0)
	for _, s := range byID {
		st := parseRFCNano(s.Timestamp)
		en := st + s.Duration
		if minStart == 0 || st < minStart {
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
	for id := range byID {
		if _, ok := depth[id]; !ok {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		if _, ok := depth[id]; ok {
			continue
		}
		warnings = append(warnings, "disconnected graph component at span: "+id)
		visit(id, 0)
	}
	services := map[string]bool{}
	for id, s := range byID {
		st := parseRFCNano(s.Timestamp)
		own := spanInterval{st, st + s.Duration}
		intervals := []spanInterval{}
		for _, cid := range children[id] {
			c := byID[cid]
			cs := parseRFCNano(c.Timestamp)
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
		resp.Spans = append(resp.Spans, w)
		services[s.ServiceName] = true
		if s.StatusCode == "STATUS_CODE_ERROR" {
			resp.Summary.ErrorCount++
		}
		if depth[id] > resp.Summary.MaxDepth {
			resp.Summary.MaxDepth = depth[id]
		}
		if id == selectedID {
			resp.SelectedSpan = &WaterfallSelectedSpan{SpanID: id, ResourceAttributes: s.ResourceAttributes, SpanAttributes: s.SpanAttributes, Events: s.Events, Links: s.Links}
		}
	}
	sort.Slice(resp.Spans, func(i, j int) bool {
		if resp.Spans[i].StartOffsetMs != resp.Spans[j].StartOffsetMs {
			return resp.Spans[i].StartOffsetMs < resp.Spans[j].StartOffsetMs
		}
		return resp.Spans[i].SpanID < resp.Spans[j].SpanID
	})
	resp.SlowestSpans = topWaterfall(resp.Spans, func(s WaterfallSpan) float64 { return s.DurationMs })
	resp.LargestSelfTimeContributors = topWaterfall(resp.Spans, func(s WaterfallSpan) float64 { return s.SelfTimeMs })
	resp.Summary.SpanCount = len(resp.Spans)
	resp.Summary.ServiceCount = len(services)
	resp.Summary.RootSpanIDs = roots
	resp.Summary.DurationMs = float64(maxEnd-minStart) / 1e6
	if minStart > 0 {
		resp.Summary.Start = time.Unix(0, minStart).UTC().Format(time.RFC3339Nano)
		resp.Summary.End = time.Unix(0, maxEnd).UTC().Format(time.RFC3339Nano)
	}
	resp.Evidence.ReturnedSpans = len(resp.Spans)
	resp.Evidence.Warnings = warnings
	resp.Interpretation.EvidenceQuality = "high"
	if resp.Evidence.Truncated || len(warnings) > 0 {
		resp.Interpretation.EvidenceQuality = "medium"
	}
	resp.Interpretation.Summary = "Compact trace waterfall with interval-union self-time. Slowest spans and self-time contributors are observations, not proof of cause."
	resp.Interpretation.Limitations = []string{"V1 does not compute or claim a critical path."}
	if resp.Spans == nil {
		resp.Spans = []WaterfallSpan{}
	}
	return resp
}

func parseRFCNano(v string) int64 {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return 0
	}
	return t.UnixNano()
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
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
