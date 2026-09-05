package apm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// get_service_performance_details used to reuse the full requested
// window as the inner PromQL range-vector selector on ~9 sub-queries
// (e.g. rate(x[<full window>])), which makes the backend rescan the entire
// window's raw samples per output step. That crashes/OOMs the backend for
// windows wider than ~1-2 weeks. Confirmed via direct backend testing:
//   - the backend hard-caps a single query's outer range at 35 days (HTTP
//     422 "Too many samples queried" just past that).
//   - splitting a wider outer window into <=35-day chunks and querying each
//     with a step-derived inner selector works cleanly.
//
// $__rate_interval resolves to max(step + scrape, 4 x scrape), so a selector
// is never narrower than its step (overlap stays near 1x) and never narrower
// than the scrape interval — the second floor is what keeps a narrow window's
// rate() from returning nothing. Measured against the backend, not assumed.
const (
	// maxServicePerformanceWindowDays is NOT a data-retention cutoff — a
	// chunk entirely outside retention just comes back empty from the
	// backend, which chunking already handles fine. This is purely a
	// call-count/latency guard: each chunk costs up to ~9 backend calls, so
	// this bounds how many chunks (and how long) a single tool call can take.
	maxServicePerformanceWindowDays = 366
	// perfDetailsMaxChunkDays is the backend's confirmed hard per-query range
	// cap. Windows wider than this are split into consecutive chunks of at
	// most this many days.
	perfDetailsMaxChunkDays = 35
	// perfDetailsDefaultTopN is the default number of entries returned for
	// the three top-k fields (top_operations_by_response_time,
	// top_operations_by_error_rate, top_errors) when top_n is unset/zero.
	perfDetailsDefaultTopN = 10
	// perfDetailsMaxTopN caps top_n so a huge requested value can't blow up
	// the backend topk()/over-fetch cost (e.g. topk(2000000, ...) for
	// top_n: 1000000). Clamps down rather than erroring, matching the
	// clamp-not-error precedent in service_summary.go's serviceSummaryMaxLimit.
	perfDetailsMaxTopN = 100
	// perfDetailsMaxConcurrency bounds how many chunks are fanned out to the
	// backend in parallel, matching the small bounded concurrency other
	// chunked callers (get_logs/get_traces) use.
	perfDetailsMaxConcurrency = 5
	// Caps output timestamps per series per chunk. Only safe because every
	// rate() selector is sized from the resulting step via $__rate_interval;
	// the apdex and response-time queries carry no rate() and just resolve as
	// last-value at the wider step. The cap only binds past ~3h20m of window,
	// and applies per chunk - a chunked window merges to ~200 x chunk count.
	perfDetailsMaxDataPoints = 200
)

// firstNonEmpty returns the first non-empty string, enabling canonical-wins
// resolution between a canonical param and its alias.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type apiPromInstantResp []struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

type apiPromRangeResp []struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

// Input structs for MCP SDK handlers
type ServiceEnvironmentsArgs struct {
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Optional service name to filter environments for (e.g. my-api). When omitted, returns environments across all services."`
}

type ServicePerformanceDetailsArgs struct {
	ServiceName     string  `json:"service_name" jsonschema:"Name of the service to get performance details for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
	TopN            int     `json:"top_n,omitempty" jsonschema:"Max entries to return for top_operations_by_response_time, top_operations_by_error_rate, and top_errors (default: 10). Values above 100 clamp to 100."`
}

type ServiceOperationsSummaryArgs struct {
	ServiceName     string  `json:"service_name" jsonschema:"Name of the service to get operations summary for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
}

type ServiceDependencyGraphArgs struct {
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by (default: .*, e.g. prod)"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Service name to focus on in the dependency graph (e.g. api-service)"`
}

type PromqlRangeQueryArgs struct {
	Query           string  `json:"query" jsonschema:"PromQL query to execute (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlInstantQueryArgs struct {
	Query           string  `json:"query" jsonschema:"PromQL query to execute (required)"`
	TimeISO         string  `json:"time_iso,omitempty" jsonschema:"Evaluation time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). If omitted, defaults to now or now-lookback_minutes."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now when time_iso is omitted (default: 0, minimum: 1)."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlLabelValuesArgs struct {
	MatchQuery      string  `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Match           string  `json:"match,omitempty" jsonschema:"Alias of match_query (matches the Prometheus API's match parameter); ignored when match_query is set."`
	Label           string  `json:"label" jsonschema:"Label name to get values for (required)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type PromqlLabelsArgs struct {
	MatchQuery      string  `json:"match_query,omitempty" jsonschema:"PromQL query to match series (e.g. up{job=\"prometheus\"})"`
	Match           string  `json:"match,omitempty" jsonschema:"Alias of match_query (matches the Prometheus API's match parameter); ignored when match_query is set."`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2024-06-01T12:00:00Z). Optional when lookback_minutes is provided."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2024-06-01T13:00:00Z). Defaults to now when omitted."`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, minimum: 1). Use for relative windows like last 30 minutes."`
	Datasource      string  `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

func resolveTimeRange(startTimeISO, endTimeISO string, lookbackMinutes float64) (int64, int64, error) {
	params := map[string]interface{}{}
	if startTimeISO != "" {
		params["start_time_iso"] = startTimeISO
	}
	if endTimeISO != "" {
		params["end_time_iso"] = endTimeISO
	}
	if lookbackMinutes != 0 {
		params["lookback_minutes"] = lookbackMinutes
	}

	startTime, endTime, err := utils.GetTimeRange(params, utils.DefaultLookbackMinutes)
	if err != nil {
		return 0, 0, err
	}

	return startTime.Unix(), endTime.Unix(), nil
}

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

	return time.Now().UTC().Unix(), nil
}

type TimeSeriesPoint struct {
	Timestamp uint64  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type TimeSeries struct {
	Metric map[string]string `json:"metric"`
	Values []TimeSeriesPoint `json:"values"`
}

type PromRangeResponse struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

func parsePromTimeSeries(respBody []byte) ([]TimeSeries, error) {
	var promResp []PromRangeResponse
	var resp []TimeSeries
	if err := json.Unmarshal(respBody, &promResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Prometheus response: %w", err)
	}
	// Convert Prometheus response to TimeSeries format
	for _, r := range promResp {
		series := TimeSeries{
			Metric: r.Metric,
			Values: make([]TimeSeriesPoint, 0, len(r.Values)),
		}
		for _, v := range r.Values {
			if len(v) != 2 {
				return nil, fmt.Errorf("invalid value format in Prometheus response: %v", v)
			}
			if ts, ok := v[0].(float64); ok {
				if valStr, ok := v[1].(string); ok {
					val, err := strconv.ParseFloat(valStr, 64)
					if err != nil {
						return nil, fmt.Errorf("failed to parse value: %w", err)
					}
					point := TimeSeriesPoint{
						Timestamp: uint64(ts),
						Value:     val,
					}
					series.Values = append(series.Values, point)
				} else {
					return nil, fmt.Errorf("invalid value type in Prometheus response: %T", v[1])
				}
			} else {
				return nil, fmt.Errorf("invalid timestamp type in Prometheus response: %T", v[0])
			}
		}
		resp = append(resp, series)
	}
	return resp, nil
}

type ServiceOperationsSummaryResponse struct {
	ServiceName string                    `json:"service_name"`
	Env         string                    `json:"env"`
	Operations  []ServiceOperationSummary `json:"operations"`
}

type ServiceOperationSummary struct {
	Name            string             `json:"name"`
	ServiceName     string             `json:"service_name"`
	Env             string             `json:"env"`
	DBSystem        string             `json:"db_system,omitempty"`
	MessagingSystem string             `json:"messaging_system,omitempty"`
	NetPeerName     string             `json:"net_peer_name,omitempty"`
	RPCSystem       string             `json:"rpc_system,omitempty"`
	Throughput      float64            `json:"throughput"`
	ErrorRate       float64            `json:"error_rate"`
	ResponseTime    map[string]float64 `json:"response_time"`
	ErrorPercent    float64            `json:"error_percent"`
}

type ServicePerformanceDetails struct {
	ServiceName   string       `json:"service_name"`
	Env           string       `json:"env"`
	Throughput    []TimeSeries `json:"throughput"` // by status code
	ErrorRate     []TimeSeries `json:"error_rate"` // by status code
	ErrorPercent  []TimeSeries `json:"error_percentage"`
	ResponseTimes []TimeSeries `json:"response_times"` // p50, p90, p95, avg, max
	ApdexScore    []TimeSeries `json:"apdex_score"`
	Availability  []TimeSeries `json:"availability"`
	TopOperations struct {
		ByResponseTime []map[string]float64 `json:"by_response_time"`
		ByErrorRate    []map[string]int64   `json:"by_error_rate"`
	} `json:"top_operations"`
	TopErrors     []map[string]int64 `json:"top_errors"`
	PartialErrors []string           `json:"partial_errors,omitempty"`
}

type perfDetailsChunk struct {
	start int64
	end   int64
}

// splitIntoPerfDetailsChunks splits [start, end) into as-close-to-equal-width
// consecutive chunks (all within 1 second of each other), each no wider than
// perfDetailsMaxChunkDays. Chunk boundaries are inclusive on both ends (chunk
// N's end equals chunk N+1's start), matching how the range-query results at
// those boundaries overlap.
//
// Equal widths matter beyond just call-count: the backend derives each
// chunk's output step server-side from the `window` param sent with that
// chunk's query (no explicit step is sent), so unequal chunk widths can come
// back on different-resolution time grids, breaking the boundary dedup in
// mergeChunkedSeries. Splitting into "as many full-width chunks as fit, then
// a narrower remainder" (the old behavior) produces exactly that mismatch.
func splitIntoPerfDetailsChunks(start, end int64) []perfDetailsChunk {
	const maxChunkSeconds = int64(perfDetailsMaxChunkDays) * 24 * 3600
	total := end - start
	if total <= maxChunkSeconds {
		return []perfDetailsChunk{{start: start, end: end}}
	}

	numChunks := (total + maxChunkSeconds - 1) / maxChunkSeconds // ceil(total/max)
	baseWidth := total / numChunks
	remainder := total % numChunks

	chunks := make([]perfDetailsChunk, 0, numChunks)
	cur := start
	for i := int64(0); i < numChunks; i++ {
		width := baseWidth
		if i < remainder {
			width++ // distribute the remainder across the first `remainder` chunks
		}
		chunkEnd := cur + width
		chunks = append(chunks, perfDetailsChunk{start: cur, end: chunkEnd})
		cur = chunkEnd
	}
	return chunks
}

func chunkBoundsLabel(c perfDetailsChunk) string {
	return fmt.Sprintf("chunk %s..%s",
		time.Unix(c.start, 0).UTC().Format(time.RFC3339),
		time.Unix(c.end, 0).UTC().Format(time.RFC3339))
}

// seriesLabelKey builds a stable, order-independent key for a metric label
// set so series from different chunks can be matched by label content
// rather than by their position in each chunk's response array.
func seriesLabelKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte(';')
	}
	return b.String()
}

// mergeChunkedSeries stitches per-chunk TimeSeries results (in chunk order)
// into one series per unique label set, dropping any incoming point at or
// before the last timestamp already appended. A label set need not appear in
// every chunk.
//
// Adjacent chunks aren't guaranteed to land on the same output time grid
// (the backend derives step from each chunk's own `window` param), so an
// exact-equality check at the boundary can miss a mismatch and let
// near-duplicate/out-of-order points through. A `<=` comparison is robust to
// that regardless of whether the grids happen to align.
func mergeChunkedSeries(chunkResults [][]TimeSeries) []TimeSeries {
	merged := map[string]*TimeSeries{}
	var order []string
	for _, chunkSeries := range chunkResults {
		for _, s := range chunkSeries {
			key := seriesLabelKey(s.Metric)
			existing, ok := merged[key]
			if !ok {
				cp := TimeSeries{Metric: s.Metric, Values: make([]TimeSeriesPoint, 0, len(s.Values))}
				for _, v := range s.Values {
					if len(cp.Values) > 0 && v.Timestamp <= cp.Values[len(cp.Values)-1].Timestamp {
						continue
					}
					cp.Values = append(cp.Values, v)
				}
				merged[key] = &cp
				order = append(order, key)
				continue
			}
			for _, v := range s.Values {
				if len(existing.Values) > 0 && v.Timestamp <= existing.Values[len(existing.Values)-1].Timestamp {
					continue
				}
				existing.Values = append(existing.Values, v)
			}
		}
	}
	result := make([]TimeSeries, 0, len(order))
	for _, key := range order {
		result = append(result, *merged[key])
	}
	return result
}

// toUtilsChunks converts our seconds-based perfDetailsChunk into the
// milliseconds-based utils.TimeChunk RunChunksParallel expects. Order is
// preserved so result index still maps 1:1 back to the original chunk.
func toUtilsChunks(chunks []perfDetailsChunk) []utils.TimeChunk {
	out := make([]utils.TimeChunk, len(chunks))
	for i, c := range chunks {
		out[i] = utils.TimeChunk{StartMs: c.start * 1000, EndMs: c.end * 1000}
	}
	return out
}

// chunkWindowSelector renders a chunk's own width as a PromQL range-vector
// selector duration (e.g. "3m"), clamped to a 1-minute minimum. Truncating
// integer minutes on a sub-60s-wide chunk would otherwise render as the
// invalid "[0m]" selector.
func chunkWindowSelector(c perfDetailsChunk) string {
	widthMinutes := (c.end - c.start) / 60
	if widthMinutes < 1 {
		widthMinutes = 1
	}
	return fmt.Sprintf("%dm", widthMinutes)
}

// chunkStatusError marks a sub-query failure as a non-2xx HTTP status
// response (as opposed to a read or parse error) so the single-chunk path in
// fetchChunkedRangeSeries/fetchChunkedTopK can treat it as soft.
type chunkStatusError struct{ err error }

func (e *chunkStatusError) Error() string { return e.err.Error() }
func (e *chunkStatusError) Unwrap() error { return e.err }

// fetchChunkedRangeSeries runs a range-vector query once per chunk (via
// buildQuery) in parallel (bounded by perfDetailsMaxConcurrency) and merges
// the results with mergeChunkedSeries.
//
// The constants.PerChunkHTTPTimeout bound is applied per chunk only for
// genuinely chunked (>1 chunk) calls, so one hung chunk can't stall a wide
// multi-chunk fan-out. A single-chunk (<=35 day) call instead runs under the
// caller's own ambient context with no added timeout, exactly matching
// pre-chunking behavior (this handler previously passed the request context
// straight through with no per-call bound).
//
// For a single-chunk (unchunked, <=35 day) call: a read error or a parse
// error aborts the whole call immediately, matching the pre-chunking
// behavior exactly (same error text/wrapping via parseErrMsg). A non-2xx
// status response instead stays soft — appended to partialErrors (with no
// chunk-bounds prefix, since there's only one "chunk") and the call still
// succeeds — which also matches the pre-chunking behavior exactly (non-2xx
// was always soft there too, before chunking existed). For a genuinely
// chunked (>1 chunk) call, all three failure kinds on any one chunk are
// recorded in partialErrors (with that chunk's time bounds) and skipped so
// the rest keep merging.
func fetchChunkedRangeSeries(ctx context.Context, client *http.Client, cfg models.Config, chunks []perfDetailsChunk, buildQuery func(perfDetailsChunk) string, label, parseErrMsg string, partialErrors *[]string) ([]TimeSeries, error) {
	singleChunk := len(chunks) == 1
	results := utils.RunChunksParallel(ctx, toUtilsChunks(chunks), perfDetailsMaxConcurrency,
		func(cctx context.Context, idx int, _ utils.TimeChunk) ([]TimeSeries, error) {
			c := chunks[idx]
			chunkCtx := cctx
			if !singleChunk {
				var cancel context.CancelFunc
				chunkCtx, cancel = context.WithTimeout(cctx, constants.PerChunkHTTPTimeout)
				defer cancel()
			}

			httpResp, err := utils.MakePromRangeAPIQuery(
				chunkCtx, client, buildQuery(c), c.start, c.end, cfg,
				utils.PromResolution{MaxDataPoints: perfDetailsMaxDataPoints},
			)
			if err != nil {
				return nil, err
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode != http.StatusOK {
				return nil, &chunkStatusError{promErr(httpResp, label)}
			}
			data, err := io.ReadAll(httpResp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}
			seriesList, err := parsePromTimeSeries(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", parseErrMsg, err)
			}
			return seriesList, nil
		})

	var chunkResults [][]TimeSeries
	for _, r := range results {
		if r.Err != nil {
			var statusErr *chunkStatusError
			if singleChunk {
				if !errors.As(r.Err, &statusErr) {
					return nil, r.Err
				}
				*partialErrors = append(*partialErrors, r.Err.Error())
				continue
			}
			*partialErrors = append(*partialErrors, fmt.Sprintf("%s: %s", chunkBoundsLabel(chunks[r.Index]), r.Err.Error()))
			continue
		}
		chunkResults = append(chunkResults, r.Value)
	}
	return mergeChunkedSeries(chunkResults), nil
}

// fetchChunkedTopK runs an instant-query top-k-style query once per chunk
// (via buildQuery, given the chunk and its own clamped width selector) in
// parallel, decodes each chunk's response into a flat list of single-key
// maps via keyFn/parseVal, and returns the per-chunk results unmerged —
// callers apply the domain-specific merge (mergeTopFloat/mergeTopInt64).
//
// Failure handling matches fetchChunkedRangeSeries: on the single-chunk
// path, a read/decode error aborts immediately, while a non-2xx status
// response stays soft (partial error, no chunk-bounds prefix, call still
// succeeds); a multi-chunk call records a partial error per failing chunk
// (with that chunk's time bounds) and continues with the rest.
//
// Like fetchChunkedRangeSeries, the constants.PerChunkHTTPTimeout bound is
// applied per chunk only for genuinely chunked (>1 chunk) calls; a
// single-chunk call runs under the caller's own ambient context with no
// added timeout, matching pre-chunking behavior.
func fetchChunkedTopK[V float64 | int64](
	ctx context.Context,
	client *http.Client,
	cfg models.Config,
	chunks []perfDetailsChunk,
	buildQuery func(c perfDetailsChunk, windowSelector string) string,
	keyFn func(m map[string]string) (string, bool),
	parseVal func(s string) (V, bool),
	label string,
	partialErrors *[]string,
) ([][]map[string]V, error) {
	singleChunk := len(chunks) == 1
	results := utils.RunChunksParallel(ctx, toUtilsChunks(chunks), perfDetailsMaxConcurrency,
		func(cctx context.Context, idx int, _ utils.TimeChunk) ([]map[string]V, error) {
			c := chunks[idx]
			chunkCtx := cctx
			if !singleChunk {
				var cancel context.CancelFunc
				chunkCtx, cancel = context.WithTimeout(cctx, constants.PerChunkHTTPTimeout)
				defer cancel()
			}

			query := buildQuery(c, chunkWindowSelector(c))
			httpResp, err := utils.MakePromInstantAPIQuery(chunkCtx, client, query, c.end, cfg)
			if err != nil {
				return nil, err
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode != http.StatusOK {
				return nil, &chunkStatusError{promErr(httpResp, label)}
			}
			var resp apiPromInstantResp
			if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
				return nil, fmt.Errorf("failed to decode %s response: %w", label, err)
			}
			items := make([]map[string]V, 0, len(resp))
			for _, r := range resp {
				key, ok := keyFn(r.Metric)
				if !ok {
					continue
				}
				if valStr, ok := r.Value[1].(string); ok {
					if val, ok := parseVal(valStr); ok {
						items = append(items, map[string]V{key: val})
					}
				}
			}
			return items, nil
		})

	var chunkItems [][]map[string]V
	for _, r := range results {
		if r.Err != nil {
			var statusErr *chunkStatusError
			if singleChunk {
				if !errors.As(r.Err, &statusErr) {
					return nil, r.Err
				}
				*partialErrors = append(*partialErrors, r.Err.Error())
				continue
			}
			*partialErrors = append(*partialErrors, fmt.Sprintf("%s: %s", chunkBoundsLabel(chunks[r.Index]), r.Err.Error()))
			continue
		}
		chunkItems = append(chunkItems, r.Value)
	}
	return chunkItems, nil
}

// mergeTopFloat merges topk-style instant-query results (one single-key map
// per operation) across chunks, keeping the max value seen per key, then
// re-sorts descending and truncates to limit.
func mergeTopFloat(chunkResults [][]map[string]float64, limit int) []map[string]float64 {
	best := map[string]float64{}
	var order []string
	for _, items := range chunkResults {
		for _, m := range items {
			for k, v := range m {
				if cur, ok := best[k]; !ok || v > cur {
					if !ok {
						order = append(order, k)
					}
					best[k] = v
				}
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return best[order[i]] > best[order[j]] })
	if len(order) > limit {
		order = order[:limit]
	}
	result := make([]map[string]float64, 0, len(order))
	for _, k := range order {
		result = append(result, map[string]float64{k: best[k]})
	}
	return result
}

// mergeTopInt64 merges topk-style instant-query results for count-style
// metrics (sum_over_time occurrence counts, e.g. topErrQuery/topErrorsQuery)
// across chunks. Unlike mergeTopFloat (max-merge, correct for percentile/
// worst-case-style values like topRTQuery's p95 latency), counts must be
// SUMMED per key across chunks — a key's total count is the sum of its
// per-chunk counts, not the max of them. Re-sorts descending and truncates
// to limit.
func mergeTopInt64(chunkResults [][]map[string]int64, limit int) []map[string]int64 {
	total := map[string]int64{}
	var order []string
	for _, items := range chunkResults {
		for _, m := range items {
			for k, v := range m {
				if _, ok := total[k]; !ok {
					order = append(order, k)
				}
				total[k] += v
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return total[order[i]] > total[order[j]] })
	if len(order) > limit {
		order = order[:limit]
	}
	result := make([]map[string]int64, 0, len(order))
	for _, k := range order {
		result = append(result, map[string]int64{k: total[k]})
	}
	return result
}

func NewServicePerformanceDetailsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServicePerformanceDetailsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServicePerformanceDetailsArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Handle environment
		env := args.Env
		if env == "" {
			env = ".*"
		}

		// Handle service_name
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}

		windowSeconds := endTimeParam - startTimeParam
		if windowSeconds > int64(maxServicePerformanceWindowDays)*24*3600 {
			// Ceiling-divide so the reported "got X days" is always strictly
			// greater than the max at the rejection boundary (e.g. exactly
			// 366 days + 1 second must report 367 days, not 366 — otherwise
			// the message says "got 366 days, max is 366 days" with no
			// visible reason for the rejection).
			gotDays := (windowSeconds + 24*3600 - 1) / (24 * 3600)
			return nil, nil, fmt.Errorf(
				"time range too wide for get_service_performance_details: got %d days, max is %d days (this call fans out to multiple sub-queries per %d-day chunk; this bound limits call count/latency, not data availability)",
				gotDays, maxServicePerformanceWindowDays, perfDetailsMaxChunkDays,
			)
		}

		chunks := splitIntoPerfDetailsChunks(startTimeParam, endTimeParam)
		multiChunk := len(chunks) > 1

		topN := args.TopN
		if topN <= 0 {
			topN = perfDetailsDefaultTopN
		}
		if topN > perfDetailsMaxTopN {
			topN = perfDetailsMaxTopN
		}

		details := ServicePerformanceDetails{
			ServiceName: serviceName,
			Env:         env,
		}

		// Get Apdex Score over time range as a vector
		details.ApdexScore, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"sum(trace_service_apdex_score{service_name='%s', env=~'%s'})",
				serviceName, env,
			)
		}, "service performance details apdex", "apdex score", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Response Times - keep vector output
		details.ResponseTimes, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"sum by (quantile) (trace_service_response_time{service_name='%s', env='%s'}[$__rate_interval])",
				serviceName, env,
			)
		}, "service performance details response_times", "response times", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Availability over time range as a vector
		details.Availability, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"(1 - (sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) or 0) / (sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) + 0.0000001)) * 100 default -999",
				serviceName, env, serviceName, env,
			)
		}, "service performance details availability", "availability response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Throughput by status code - keep vector output
		details.Throughput, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"sum by (http_status_code)(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) * 60 default 0",
				serviceName, env,
			)
		}, "service performance details throughput", "throughput response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Error Rate by status code - keep vector output
		details.ErrorRate, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"sum by (service_name, http_status_code)(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) * 60 default 0",
				serviceName, env,
			)
		}, "service performance details error_rate", "error rate response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Calculate Error Percentage over time range as a vector
		details.ErrorPercent, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				"(sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) / sum(rate(trace_endpoint_count{service_name='%s', env='%s', span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) * 100) default 0",
				serviceName, env, serviceName, env,
			)
		}, "service performance details error_percent", "error percent response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// spanKindKeyFn builds the unique key topRTQuery/topErrQuery use
		// (span_name plus its co-selected label fields); every row from
		// those queries carries all of them, so it never skips a row.
		spanKindKeyFn := func(m map[string]string) (string, bool) {
			return fmt.Sprintf("%s-%s-%s-%s-%s-%s-%s",
				m["span_name"], m["span_kind"], m["net_peer_name"],
				m["db_system"], m["rpc_system"], m["messaging_system"],
				m["process_runtime_name"],
			), true
		}
		parseFloatOK := func(s string) (float64, bool) {
			v, err := strconv.ParseFloat(s, 64)
			return v, err == nil
		}
		parseIntOK := func(s string) (int64, bool) {
			v, err := strconv.ParseInt(s, 10, 64)
			return v, err == nil
		}

		// Get Top Operations by Response Time - keep vector output. This is
		// an instant query whose only windowing mechanism is the inner
		// quantile_over_time([...]) selector, so (unlike the rate() queries
		// above) it must stay at the chunk's own width, not the step-derived
		// $__rate_interval the range queries use.
		topRTLimit := topN
		if multiChunk {
			topRTLimit = 2 * topN // over-fetch per chunk so the cross-chunk merge still has topN real candidates
		}
		topRTChunks, err := fetchChunkedTopK(ctx, client, cfg, chunks,
			func(c perfDetailsChunk, windowSelector string) string {
				return fmt.Sprintf(
					"topk(%d, quantile_over_time(0.95, sum by (span_name, messaging_system, rpc_system, span_kind,net_peer_name,process_runtime_name,db_system)(trace_endpoint_duration{service_name='%s', span_kind!='SPAN_KIND_INTERNAL', env='%s', quantile='p95'}[%s])))",
					topRTLimit, serviceName, env, windowSelector,
				)
			},
			spanKindKeyFn, parseFloatOK,
			"service performance details top_operations_by_response_time", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}
		if len(topRTChunks) > 0 {
			details.TopOperations.ByResponseTime = mergeTopFloat(topRTChunks, topN)
		}

		// Get Top Operations by Error Rate - keep vector output. Unlike
		// topRTQuery, this query is not wrapped in topk() (it already
		// returns every matching series unbounded); mergeTopInt64 sorts and
		// truncates to topN uniformly, whether there's one chunk or many.
		topErrChunks, err := fetchChunkedTopK(ctx, client, cfg, chunks,
			func(c perfDetailsChunk, windowSelector string) string {
				return fmt.Sprintf(
					`sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_client_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_client_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s]))`,
					serviceName, env, windowSelector, serviceName, env, windowSelector, serviceName, env, windowSelector, serviceName, env, windowSelector,
				)
			},
			spanKindKeyFn, parseIntOK,
			"service performance details top_operations_by_error_rate", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}
		if len(topErrChunks) > 0 {
			details.TopOperations.ByErrorRate = mergeTopInt64(topErrChunks, topN)
		}

		// Get Top Errors - keep vector output. Same unbounded (no topk)
		// shape as topErrQuery above; mergeTopInt64 sorts and truncates to
		// topN uniformly, whether there's one chunk or many.
		topErrorsKeyFn := func(m map[string]string) (string, bool) {
			if exceptionType := m["exception_type"]; exceptionType != "" {
				return exceptionType, true
			}
			if httpStatusCode := m["http_status_code"]; httpStatusCode != "" {
				return httpStatusCode, true
			}
			return "", false // skip if neither is present
		}
		topErrorsChunks, err := fetchChunkedTopK(ctx, client, cfg, chunks,
			func(c perfDetailsChunk, windowSelector string) string {
				return fmt.Sprintf(
					`sum by (exception_type)(sum by (exception_type, span_kind)(sum_over_time(trace_client_count{service_name="%s", env='%s', exception_type!=''}[%s])) or
					 sum by (exception_type, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', exception_type!=''}[%s]))) or
					 sum by (http_status_code)(sum by (http_status_code, span_kind)(sum_over_time(trace_client_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])) or
					 sum by (http_status_code, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env='%s', http_status_code=~"^[45].*"}[%s])))`,
					serviceName, env, windowSelector, serviceName, env, windowSelector, serviceName, env, windowSelector, serviceName, env, windowSelector,
				)
			},
			topErrorsKeyFn, parseIntOK,
			"service performance details top_errors", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}
		if len(topErrorsChunks) > 0 {
			details.TopErrors = mergeTopInt64(topErrorsChunks, topN)
		}

		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAPMServiceLink(startTimeParam*1000, endTimeParam*1000, serviceName, deeplink.APMCatalogEnvExact(env), "")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

func NewServiceOperationsSummaryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceOperationsSummaryArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		env := args.Env
		if env == "" {
			env = ".*" // default environment
		}
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}
		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))
		// Prepare the Prometheus query for throughput of endpoint operations
		throughputQuery := fmt.Sprintf(
			"sum by (span_name, span_kind)(sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare instant query request to Prometheus
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, throughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var promResp apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&promResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of endpoint operations
		respTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, span_kind) (trace_endpoint_duration{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, respTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var respTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&respTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of endpoint operations
		errorRateQuery := fmt.Sprintf(
			"100 * (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s', http_status_code=~'4.*|5.*'}[%s])) / %d) / (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='%s', span_kind='SPAN_KIND_SERVER', env=~'%s'}[%s])) / %d)",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, errorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var errorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&errorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for throughput of database operations
		dbThroughputQuery := fmt.Sprintf(
			"sum by (span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', span_kind='SPAN_KIND_CLIENT', db_system!='', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of database operations
		dbRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, db_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', span_kind='SPAN_KIND_CLIENT', db_system!='', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of database operations
		dbErrorRateQuery := fmt.Sprintf(
			`
			    100 * 
    			(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
					or
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
				)  
				/ 
				(
					sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
						(sum_over_time(trace_client_count{service_name="%s", db_system!="",env=~"%s"} [%s]) / %d)
				)
			`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, dbErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var dbErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&dbErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for http operations
		httpThroughputQuery := fmt.Sprintf(
			"sum by(span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', span_kind='SPAN_KIND_CLIENT', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of http operations
		httpRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', span_kind='SPAN_KIND_CLIENT', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of http operations
		httpErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s", status_code=~"STATUS_CODE_ERROR"} [%s]) / %d)
				or
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s", http_status_code=~"4.*|5.*"} [%s]) / %d)
			)
			/
			(
				sum by(span_name, db_system, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", env=~"%s"} [%s]) / %d)
			)`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, httpErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var httpErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&httpErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare query for messaging operations
		messagingThroughputQuery := fmt.Sprintf(
			"sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='%s', messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for response times of messaging operations
		messagingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95, sum by (quantile, span_name, messaging_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='%s', messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the Prometheus query for error rate of messaging operations
		messagingErrorRateQuery := fmt.Sprintf(
			`			100 * 
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", status_code=~"STATUS_CODE_ERROR", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
				or
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", http_status_code=~"4.*|5.*", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)
			/
			(
				sum by(span_name, messaging_system, net_peer_name, rpc_system, span_kind)
					(sum_over_time(trace_client_count{service_name="%s", messaging_system!="", env=~"%s", span_kind='SPAN_KIND_PRODUCER'} [%s]) / %d)
			)`,
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		// Prepare request to Prometheus (or your metrics backend)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, messagingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service operations summary")
		}
		var messagingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&messagingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Prepare the response structure
		operationsSummary := make([]ServiceOperationSummary, 0)
		for _, r := range promResp {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range respTimeRaw {
				if rt.Metric["span_name"] == operation.Name {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}

			// Find matching error rate data
			for _, er := range errorRateRaw {
				if er.Metric["span_name"] == operation.Name {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// Add database operations
		for _, r := range dbThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				DBSystem:    r.Metric["db_system"],
				NetPeerName: r.Metric["net_peer_name"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range dbRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["db_system"] == operation.DBSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range dbErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["db_system"] == operation.DBSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add http operations
		for _, r := range httpThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:        r.Metric["span_name"],
				ServiceName: serviceName,
				Env:         env,
				NetPeerName: r.Metric["net_peer_name"],
				RPCSystem:   r.Metric["rpc_system"],
				Throughput:  0, // default to 0, will be updated later
				ErrorRate:   0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range httpRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range httpErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// add messaging operations
		for _, r := range messagingThroughputRaw {
			// Extract operation details
			operation := ServiceOperationSummary{
				Name:            r.Metric["span_name"],
				ServiceName:     serviceName,
				Env:             env,
				MessagingSystem: r.Metric["messaging_system"],
				NetPeerName:     r.Metric["net_peer_name"],
				RPCSystem:       r.Metric["rpc_system"],
				Throughput:      0, // default to 0, will be updated later
				ErrorRate:       0, // default to 0, will be updated later
				ResponseTime: map[string]float64{
					"p95": 0, // default to 0, will be updated later
					"p90": 0,
					"p50": 0,
					"avg": 0,
					"max": 0,
				},
				ErrorPercent: 0, // default to 0, will be updated later
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					operation.Throughput = throughputVal
				}
			}
			// Find matching response time data
			for _, rt := range messagingRespTimeRaw {
				if rt.Metric["span_name"] == operation.Name &&
					rt.Metric["messaging_system"] == operation.MessagingSystem &&
					rt.Metric["net_peer_name"] == operation.NetPeerName &&
					rt.Metric["rpc_system"] == operation.RPCSystem {
					quantile, ok := rt.Metric["quantile"]
					if !ok {
						continue // skip if quantile is not present
					}
					if valStr, ok := rt.Value[1].(string); ok {
						if val, err := strconv.ParseFloat(valStr, 64); err == nil {
							// Update the response time for the corresponding quantile
							operation.ResponseTime[quantile] = val
						}
					}
				}
			}
			// Find matching error rate data
			for _, er := range messagingErrorRateRaw {
				if er.Metric["span_name"] == operation.Name &&
					er.Metric["messaging_system"] == operation.MessagingSystem &&
					er.Metric["net_peer_name"] == operation.NetPeerName &&
					er.Metric["rpc_system"] == operation.RPCSystem {
					if valStr, ok := er.Value[1].(string); ok {
						if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
							operation.ErrorRate = errorRateVal
						}
					}
				}
			}
			// Calculate error percentage
			if operation.Throughput > 0 {
				operation.ErrorPercent = (operation.ErrorRate / operation.Throughput) * 100
			}
			operationsSummary = append(operationsSummary, operation)
		}
		// Prepare the final response structure
		details := ServiceOperationsSummaryResponse{
			ServiceName: serviceName,
			Env:         env,
			Operations:  operationsSummary,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAPMServiceLink(startTimeParam*1000, endTimeParam*1000, serviceName, deeplink.APMCatalogEnvExact(env), "operations")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

type RedMetrics struct {
	Throughput, ResponseTimeP95, ErrorRate, ErrorPercent float64
	ResponseTimeP50, ResponseTimeP90, ResponseTimeAvg    float64
	ResponseTimeMax                                      float64
}

type ServiceDependencyGraphDetails struct {
	ServiceName      string                `json:"service_name"`
	Env              string                `json:"env"`
	Incoming         map[string]RedMetrics `json:"incoming"`
	Outgoing         map[string]RedMetrics `json:"outgoing"`
	MessagingSystems map[string]RedMetrics `json:"messaging_systems"`
	Databases        map[string]RedMetrics `json:"databases"`
}

func NewServiceDependencyGraphHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceDependencyGraphArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		env := args.Env
		if env == "" {
			env = ".*" // default environment
		}
		serviceName := args.ServiceName
		if serviceName == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}
		timeRange := fmt.Sprintf("%dm", int((endTimeParam-startTimeParam)/60))

		incoming := make(map[string]RedMetrics)
		outgoing := make(map[string]RedMetrics)
		databases := make(map[string]RedMetrics)
		messagingSystems := make(map[string]RedMetrics)

		// Incoming requests (HTTP server operations):
		// throughput
		incomingThroughputQuery := fmt.Sprintf(
			"sum by (client)(sum_over_time(trace_call_graph_count{server='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, incomingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		incomingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (client, quantile) (trace_call_graph_duration{server='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		incomingErrorRateQuery := fmt.Sprintf(
			"sum by (client)(sum_over_time(trace_call_graph_count{server='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, incomingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var incomingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&incomingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process incoming data
		for _, r := range incomingThroughputRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingRespTimeRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			incoming[client] = metrics
		}
		for _, r := range incomingErrorRateRaw {
			client := r.Metric["client"]
			if client == "" {
				client = "unknown"
			}
			metrics := incoming[client]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			incoming[client] = metrics
		}
		for client, metrics := range incoming {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			incoming[client] = metrics
		}
		// Outgoing requests (HTTP client operations):
		// throughput
		outgoingThroughputQuery := fmt.Sprintf(
			"sum by (server)(sum_over_time(trace_call_graph_count{client='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		outgoingRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (server, quantile) (trace_call_graph_duration{client='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		outgoingErrorRateQuery := fmt.Sprintf(
			"sum by (server)(sum_over_time(trace_call_graph_count{client='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, outgoingErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var outgoingErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&outgoingErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process outgoing data

		for _, r := range outgoingThroughputRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := RedMetrics{}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingRespTimeRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			quantile := r.Metric["quantile"]
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			outgoing[server] = metrics
		}
		for _, r := range outgoingErrorRateRaw {
			server := r.Metric["server"]
			if server == "" {
				server = "unknown"
			}
			metrics := outgoing[server]
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			outgoing[server] = metrics
		}
		for server, metrics := range outgoing {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			outgoing[server] = metrics
		}
		// Infrastructure services:
		// throughput
		infrastructureThroughputQuery := fmt.Sprintf(
			"sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client='%s', env=~'%s'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureThroughputQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureThroughputRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureThroughputRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// response times
		infrastructureRespTimeQuery := fmt.Sprintf(
			"quantile_over_time(0.95 ,sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service, quantile) (trace_internal_call_graph_duration{client='%s', env=~'%s'}[%s]))",
			serviceName, env, timeRange,
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureRespTimeQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureRespTimeRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureRespTimeRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// error rate
		infrastructureErrorRateQuery := fmt.Sprintf(
			"sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (sum_over_time(trace_internal_call_graph_count{client='%s', env=~'%s', client_status=~'4.*|5.*'}[%s])) / %d",
			serviceName, env, timeRange, int((endTimeParam-startTimeParam)/60),
		)
		httpResp, err = utils.MakePromInstantAPIQuery(ctx, client, infrastructureErrorRateQuery, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return nil, nil, promErr(httpResp, "service dependency graph")
		}
		var infrastructureErrorRateRaw apiPromInstantResp
		if err := json.NewDecoder(httpResp.Body).Decode(&infrastructureErrorRateRaw); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
		}
		// Process infrastructure data
		for _, r := range infrastructureThroughputRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if throughputVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.Throughput = throughputVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureRespTimeRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			quantile := r.Metric["quantile"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if val, err := strconv.ParseFloat(valStr, 64); err == nil {
					switch quantile {
					case "p95":
						metrics.ResponseTimeP95 = val
					case "p90":
						metrics.ResponseTimeP90 = val
					case "p50":
						metrics.ResponseTimeP50 = val
					case "avg":
						metrics.ResponseTimeAvg = val
					case "max":
						metrics.ResponseTimeMax = val
					}
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for _, r := range infrastructureErrorRateRaw {
			host := r.Metric["server_host"]
			dbSystem := r.Metric["server_db_system"]
			rpcSystem := r.Metric["server_rpc_system"]
			messagingSystem := r.Metric["server_messaging_system"]
			rpcService := r.Metric["server_rpc_service"]
			key := ""
			metrics := RedMetrics{}
			if dbSystem != "" {
				key = fmt.Sprintf("%s %s", host, dbSystem)
				metrics = databases[key]
			} else if messagingSystem != "" {
				key = fmt.Sprintf("%s %s %s %s", host, messagingSystem, rpcSystem, rpcService)
				metrics = messagingSystems[key]
			} else {
				continue // skip if neither db_system nor messaging_system is present
			}
			if valStr, ok := r.Value[1].(string); ok {
				if errorRateVal, err := strconv.ParseFloat(valStr, 64); err == nil {
					metrics.ErrorRate = errorRateVal
				}
			}
			if dbSystem != "" {
				databases[key] = metrics
			} else if messagingSystem != "" {
				messagingSystems[key] = metrics
			}
		}
		for key, metrics := range databases {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			databases[key] = metrics
		}
		for key, metrics := range messagingSystems {
			if metrics.Throughput > 0 {
				metrics.ErrorPercent = (metrics.ErrorRate / metrics.Throughput) * 100
			}
			messagingSystems[key] = metrics
		}
		// Prepare the final response structure
		details := ServiceDependencyGraphDetails{
			ServiceName:      serviceName,
			Env:              env,
			Incoming:         incoming,
			Outgoing:         outgoing,
			Databases:        databases,
			MessagingSystems: messagingSystems,
		}
		// Return the response
		resultJSON, err := json.Marshal(details)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		dashboardURL := dlBuilder.BuildAPMServiceLink(startTimeParam*1000, endTimeParam*1000, serviceName, deeplink.APMCatalogEnvExact(env), "dependency")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(resultJSON),
				},
			},
		}, nil, nil
	}
}

// resolveDatasourceCfg returns a copy of cfg with Prometheus credentials overridden
// to those of the named datasource. If datasourceName is empty the original cfg is
// returned unchanged. Returns an error when the name is non-empty but not found.
func resolveDatasourceCfg(cfg models.Config, datasourceName string) (models.Config, error) {
	if datasourceName == "" {
		return cfg, nil
	}
	ds, ok := cfg.ResolveDatasource(datasourceName)
	if !ok {
		return cfg, fmt.Errorf("datasource %q not found", datasourceName)
	}
	if ds.ReadURL == "" || ds.Username == "" || ds.Password == "" || ds.Region == "" {
		return cfg, fmt.Errorf("datasource %q is missing required Prometheus configuration", datasourceName)
	}
	cfg.PrometheusReadURL = ds.ReadURL
	cfg.PrometheusUsername = ds.Username
	cfg.PrometheusPassword = ds.Password
	cfg.Region = ds.Region
	cfg.ClusterID = ds.ClusterID
	return cfg, nil
}

func promToolError(resp *http.Response, op string) (*mcp.CallToolResult, any, error) {
	err := utils.NewUpstreamHTTPError(resp, op)
	return utils.ToolErrorResult(err.Error()), nil, nil
}

func promErr(resp *http.Response, op string) error {
	return utils.NewUpstreamHTTPError(resp, op)
}

func NewPromqlRangeQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlRangeQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, query, startTimeParam, endTimeParam, queryCfg, utils.PromResolution{})
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus range query")
		}
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

func NewPromqlInstantQueryHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlInstantQueryArgs) (*mcp.CallToolResult, any, error) {
		query := args.Query
		if query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		timeParam, err := resolveInstantQueryTime(args.TimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromInstantAPIQuery(ctx, client, query, timeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus instant query")
		}
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// tool handler to make the query
// sum by (env)(last_over_time(domain_attributes_count))
// iterate over the values of `env` label and return the unique values
func NewServiceEnvironmentsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ServiceEnvironmentsArgs) (*mcp.CallToolResult, any, error) {
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		var matchQuery string
		if args.ServiceName != "" {
			matchQuery = fmt.Sprintf("domain_attributes_count{span_kind='SPAN_KIND_SERVER',service_name=%q}", args.ServiceName)
		} else {
			matchQuery = "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}"
		}
		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, "env", matchQuery, startTimeParam, endTimeParam, cfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus label values")
		}
		// Read the response body
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Return the environments as the content
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

func NewPromqlLabelValuesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelValuesArgs) (*mcp.CallToolResult, any, error) {
		query := firstNonEmpty(args.MatchQuery, args.Match)
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		label := args.Label
		if label == "" {
			return nil, nil, fmt.Errorf("label is required")
		}
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromLabelValuesAPIQuery(ctx, client, label, query, startTimeParam, endTimeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus label values")
		}
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

func NewPromqlLabelsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PromqlLabelsArgs) (*mcp.CallToolResult, any, error) {
		query := firstNonEmpty(args.MatchQuery, args.Match)
		if query == "" {
			return nil, nil, fmt.Errorf("match_query is required")
		}
		startTimeParam, endTimeParam, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		queryCfg, err := resolveDatasourceCfg(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}

		httpResp, err := utils.MakePromLabelsAPIQuery(ctx, client, query, startTimeParam, endTimeParam, queryCfg)
		if err != nil {
			return nil, nil, err
		}
		if httpResp == nil {
			return nil, nil, fmt.Errorf("received nil response from Prometheus")
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return promToolError(httpResp, "Prometheus labels")
		}
		// return the response body string as the content without parsing
		responseBodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(responseBodyBytes),
				},
			},
		}, nil, nil
	}
}

// ListDatasourcesArgs has no required parameters.
type ListDatasourcesArgs struct{}

// NewListDatasourcesHandler returns a handler that serves the datasource list from
// the in-memory cache populated at startup — no extra API call is made.
// The response is serialized once at registration time since the list never changes.
func NewListDatasourcesHandler(cfg models.Config) func(context.Context, *mcp.CallToolRequest, ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
	type datasourceView struct {
		Name      string `json:"name"`
		IsDefault bool   `json:"is_default"`
	}

	views := make([]datasourceView, 0, len(cfg.Datasources))
	for _, ds := range cfg.Datasources {
		views = append(views, datasourceView{
			Name:      ds.Name,
			IsDefault: ds.IsDefault,
		})
	}
	out, _ := json.Marshal(views) // slice of plain structs — cannot fail

	return func(_ context.Context, _ *mcp.CallToolRequest, _ ListDatasourcesArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(out)},
			},
		}, nil, nil
	}
}
