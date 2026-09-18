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
		// Escape once per handler: every PromQL label matcher below must use
		// escSvc/escEnv, never the raw values, to prevent label-matcher injection.
		escSvc, escEnv := utils.EscapePromQLLabel(serviceName), utils.EscapePromQLLabel(env)

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
				`sum(trace_service_apdex_score{service_name="%s", env=~"%s"})`,
				escSvc, escEnv,
			)
		}, "service performance details apdex", "apdex score", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Response Times - keep vector output
		details.ResponseTimes, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				`sum by (quantile) (trace_service_response_time{service_name="%s", env="%s"}[$__rate_interval])`,
				escSvc, escEnv,
			)
		}, "service performance details response_times", "response times", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Availability over time range as a vector
		details.Availability, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				`(1 - (sum(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) or 0) / (sum(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) + 0.0000001)) * 100 default -999`,
				escSvc, escEnv, escSvc, escEnv,
			)
		}, "service performance details availability", "availability response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Throughput by status code - keep vector output
		details.Throughput, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				`sum by (http_status_code)(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) * 60 default 0`,
				escSvc, escEnv,
			)
		}, "service performance details throughput", "throughput response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Get Error Rate by status code - keep vector output
		details.ErrorRate, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				`sum by (service_name, http_status_code)(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) * 60 default 0`,
				escSvc, escEnv,
			)
		}, "service performance details error_rate", "error rate response", &details.PartialErrors)
		if err != nil {
			return nil, nil, err
		}

		// Calculate Error Percentage over time range as a vector
		details.ErrorPercent, err = fetchChunkedRangeSeries(ctx, client, cfg, chunks, func(c perfDetailsChunk) string {
			return fmt.Sprintf(
				`(sum(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[$__rate_interval])) / sum(rate(trace_endpoint_count{service_name="%s", env="%s", span_kind='SPAN_KIND_SERVER'}[$__rate_interval])) * 100) default 0`,
				escSvc, escEnv, escSvc, escEnv,
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
					`topk(%d, quantile_over_time(0.95, sum by (span_name, messaging_system, rpc_system, span_kind,net_peer_name,process_runtime_name,db_system)(trace_endpoint_duration{service_name="%s", span_kind!='SPAN_KIND_INTERNAL', env="%s", quantile='p95'}[%s])))`,
					topRTLimit, escSvc, escEnv, windowSelector,
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
					`sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_client_count{service_name="%s", env="%s", exception_type!=''}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_endpoint_count{service_name="%s", env="%s", exception_type!=''}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_client_count{service_name="%s", env="%s", http_status_code=~"^[45].*"}[%s])) or
					 sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_endpoint_count{service_name="%s", env="%s", http_status_code=~"^[45].*"}[%s]))`,
					escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector,
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
					`sum by (exception_type)(sum by (exception_type, span_kind)(sum_over_time(trace_client_count{service_name="%s", env="%s", exception_type!=''}[%s])) or
					 sum by (exception_type, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env="%s", exception_type!=''}[%s]))) or
					 sum by (http_status_code)(sum by (http_status_code, span_kind)(sum_over_time(trace_client_count{service_name="%s", env="%s", http_status_code=~"^[45].*"}[%s])) or
					 sum by (http_status_code, span_kind)(sum_over_time(trace_endpoint_count{service_name="%s", env="%s", http_status_code=~"^[45].*"}[%s])))`,
					escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector, escSvc, escEnv, windowSelector,
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
