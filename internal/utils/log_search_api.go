package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
)

// LogSearchMaxSampleSize is the server-side maximum for log search limits.
// The log search endpoint rejects a larger limit. Clamped here so an operator's
// --max_get_logs_entries cannot turn into a failure on every request.
const LogSearchMaxSampleSize = 5000

// LogSearchRequest is one server-side log search, in the terms every log tool
// already has. Deliberately free of GetLogsArgs, deeplinks and tool-specific
// limits so a second caller adopts it by swapping one function call.
type LogSearchRequest struct {
	Pipeline any
	// StartMs and EndMs are milliseconds; the wire format is seconds.
	StartMs int64
	EndMs   int64
	// Limit of 0 is omitted. The endpoint rejects a limit on aggregate and
	// dataframe pipelines, so callers must leave it unset for those.
	Limit int
	// Index is normalized and validated here; a bare name is an error rather
	// than a silent query against the default table.
	Index string
	// Direction empty is omitted, letting the server apply its own default.
	Direction string
}

func (r LogSearchRequest) body(region string) (map[string]any, error) {
	index, err := NormalizeLogIndex(r.Index)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"region":   region,
		"start":    r.StartMs / 1000,
		"end":      r.EndMs / 1000,
		"pipeline": r.Pipeline,
	}
	if index != "" {
		body["index"] = index
	}
	if r.Direction != "" {
		body["direction"] = r.Direction
	}
	if r.Limit > 0 {
		limit := r.Limit
		if limit > LogSearchMaxSampleSize {
			limit = LogSearchMaxSampleSize
		}
		body["limit"] = limit
	}
	return body, nil
}

// MakeLogSearchAPI posts one search to the server-side log search endpoint.
// Non-200 responses are returned rather than raised, so the caller can run them
// through NewUpstreamHTTPError with its own operation name and hint.
func MakeLogSearchAPI(
	ctx context.Context, client *http.Client, cfg models.Config, req LogSearchRequest,
) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("http client cannot be nil")
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return nil, errors.New("API base URL cannot be empty")
	}
	if strings.TrimSpace(cfg.TokenManager.GetAccessToken(ctx)) == "" {
		return nil, errors.New("access token cannot be empty")
	}

	body, err := req.body(cfg.Region)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log search request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, cfg.APIBaseURL+constants.EndpointLogSearch, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setServiceLogsHeaders(httpReq, cfg.TokenManager.GetAccessToken(ctx))

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	return resp, nil
}

// densestBucketCount is how many buckets the summary keeps. Fixed so a wide
// search cannot spend unbounded context: the agent's question is "where is the
// data dense", which a handful of buckets answers as well as hundreds.
const densestBucketCount = 5

// logSearchResponse is the endpoint's body. query_result is the product: if it
// fails to decode, the whole call fails. Every other field is advisory — kept
// as raw bytes here so a malformed one can be dropped in DecodeLogSearchResponse
// instead of failing the search that already has its answer in query_result.
type logSearchResponse struct {
	QueryResult        json.RawMessage `json:"query_result"`
	Volume             json.RawMessage `json:"volume"`
	TotalMatchingLines json.RawMessage `json:"total_matching_lines"`
	LogsTruncated      json.RawMessage `json:"logs_truncated"`
	SearchStats        json.RawMessage `json:"search_stats"`
}

type logVolumeSeries struct {
	Metric map[string]string `json:"metric"`
	Values []logBucketCount  `json:"values"`
}

type logBucketCount struct {
	TS    int64
	Count float64
}

// UnmarshalJSON reads the [timestamp, count] pair. The timestamp is normally an
// integer but is tolerated as a float (truncated to seconds), and the count is
// accepted as either a JSON string or a JSON number, since the same logical
// count has been observed as "412", "412.000000", and a bare 412 depending on
// the upstream column type.
func (b *logBucketCount) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("log search: bucket count has %d elements", len(pair))
	}

	var ts float64
	if err := json.Unmarshal(pair[0], &ts); err != nil {
		return fmt.Errorf("log search: bucket timestamp: %w", err)
	}

	var count float64
	var asString string
	if err := json.Unmarshal(pair[1], &asString); err == nil {
		parsed, err := strconv.ParseFloat(asString, 64)
		if err != nil {
			return fmt.Errorf("log search: bucket count %q: %w", asString, err)
		}
		count = parsed
	} else if err := json.Unmarshal(pair[1], &count); err != nil {
		return fmt.Errorf("log search: bucket count: %w", err)
	}

	// A non-finite count marshals to nothing: json.Marshal fails on NaN and Inf,
	// so one bad bucket would sink the whole tool response at output time.
	if math.IsNaN(count) || math.IsInf(count, 0) {
		return fmt.Errorf("log search: bucket count is not finite: %v", count)
	}

	b.TS = int64(ts)
	b.Count = count
	return nil
}

// rawPresent reports whether an advisory field's raw JSON is worth decoding:
// present in the body and not an explicit null.
func rawPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// decodeQueryResult lifts the endpoint's result set. Every rejection here is
// the same failure: an envelope that decodes but carries no result reaches the
// caller as zero logs, indistinguishable from a search that matched nothing.
func decodeQueryResult(raw json.RawMessage) (map[string]any, error) {
	// A null would also nil the map, so the advisory writes later would panic.
	if !rawPresent(raw) {
		return nil, errors.New("log search response has no query_result")
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("failed to decode query_result: %w", err)
	}

	data, ok := out["data"].(map[string]any)
	if !ok {
		return nil, errors.New("log search response has no query_result.data object")
	}
	if _, ok := data["resultType"].(string); !ok {
		return nil, errors.New("log search response has no query_result.data.resultType")
	}
	// A null result is the legitimate empty-streams shape, so presence is the
	// bar here, not non-emptiness.
	if _, present := data["result"]; !present {
		return nil, errors.New("log search response has no query_result.data.result")
	}
	return out, nil
}

// DecodeLogSearchResponse flattens the endpoint's envelope into the shape
// get_logs already returns: query_result lifted to the top level, so data.result
// stays exactly where every existing consumer expects it, with the endpoint's
// extra signals attached as siblings.
//
// query_result is the only field that can fail the whole call. Every other
// field is advisory: a malformed one is simply left out of the result rather
// than discarding the log lines the caller actually asked for.
func DecodeLogSearchResponse(resp *http.Response) (map[string]any, error) {
	var parsed logSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode log search response: %w", err)
	}

	out, err := decodeQueryResult(parsed.QueryResult)
	if err != nil {
		return nil, err
	}

	var searchStats map[string]any
	if rawPresent(parsed.SearchStats) {
		if err := json.Unmarshal(parsed.SearchStats, &searchStats); err == nil {
			out["search_stats"] = searchStats
		}
	}

	if rawPresent(parsed.TotalMatchingLines) {
		var total float64
		if err := json.Unmarshal(parsed.TotalMatchingLines, &total); err == nil {
			out["total_matching_lines"] = int(total)
		}
	}

	if rawPresent(parsed.LogsTruncated) {
		var truncated bool
		if err := json.Unmarshal(parsed.LogsTruncated, &truncated); err == nil {
			out["logs_truncated"] = truncated
		}
	}

	if rawPresent(parsed.Volume) {
		var rawSeries []json.RawMessage
		if err := json.Unmarshal(parsed.Volume, &rawSeries); err == nil {
			series := make([]logVolumeSeries, 0, len(rawSeries))
			for _, rs := range rawSeries {
				var s logVolumeSeries
				// One malformed series (e.g. a bucket pair with the wrong
				// number of elements) must not sink the whole volume summary
				// or leave it half-built from that series's partial values —
				// drop just that series and keep the rest.
				if err := json.Unmarshal(rs, &s); err != nil {
					continue
				}
				series = append(series, s)
			}
			if len(series) > 0 {
				out["volume_summary"] = summarizeLogVolume(series, bucketSecondsFrom(searchStats))
			}
		}
	}

	return out, nil
}

// bucketSecondsFrom reads the bucket width the summary needs to close each
// bucket's interval. Zero when absent, which drops the end bound rather than
// inventing one.
func bucketSecondsFrom(stats map[string]any) int64 {
	seconds, ok := stats["bucket_seconds"].(float64)
	if !ok {
		return 0
	}
	return int64(seconds)
}

// summarizeLogVolume folds the per-label-set histogram into a fixed-size
// summary. The raw histogram is one series per label set over hundreds of
// buckets; passed through whole it would cost thousands of tokens on the
// most-called log tool to answer a question a handful of buckets answers.
func summarizeLogVolume(series []logVolumeSeries, bucketSeconds int64) map[string]any {
	totals := map[int64]float64{}
	for _, s := range series {
		for _, v := range s.Values {
			totals[v.TS] += v.Count
		}
	}

	buckets := make([]map[string]any, 0, len(totals))
	for ts, count := range totals {
		if count <= 0 {
			continue
		}
		bucket := map[string]any{"start": ts, "count": count}
		if bucketSeconds > 0 {
			bucket["end"] = ts + bucketSeconds
		}
		buckets = append(buckets, bucket)
	}

	// Densest first; ties break on time so the output is deterministic.
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i]["count"].(float64) != buckets[j]["count"].(float64) {
			return buckets[i]["count"].(float64) > buckets[j]["count"].(float64)
		}
		return buckets[i]["start"].(int64) < buckets[j]["start"].(int64)
	})

	summary := map[string]any{"buckets_with_data": len(buckets)}
	if len(buckets) > densestBucketCount {
		buckets = buckets[:densestBucketCount]
	}
	summary["densest"] = buckets
	return summary
}
