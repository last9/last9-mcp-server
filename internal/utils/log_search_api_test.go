package utils

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"last9-mcp/internal/models"
)

// captureLogSearchBody spins up a server that records the decoded request body
// and replies with a minimal valid search response.
func captureLogSearchBody(t *testing.T) (*httptest.Server, *map[string]any, *string) {
	t.Helper()
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"query_result":{"status":"success"},"search_stats":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &body, &path
}

func logSearchTestCfg(t *testing.T, apiBaseURL string) models.Config {
	t.Helper()
	cfg := sanityTestCfg(t, apiBaseURL)
	cfg.Region = "us-east-1"
	return cfg
}

func TestMakeLogSearchAPI_RawPipelineBody(t *testing.T) {
	srv, body, path := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	pipeline := []map[string]interface{}{{"type": "filter"}}
	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: pipeline,
		StartMs:  1_700_000_000_000,
		EndMs:    1_700_000_600_000,
		Limit:    100,
		Index:    "physical_index:my_index",
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	if *path != "/logs/query" {
		t.Errorf("path = %q, want /logs/query", *path)
	}
	// Seconds on the wire, milliseconds inside MCP.
	if got := (*body)["start"]; got != float64(1_700_000_000) {
		t.Errorf("start = %v, want 1700000000", got)
	}
	if got := (*body)["end"]; got != float64(1_700_000_600) {
		t.Errorf("end = %v, want 1700000600", got)
	}
	if got := (*body)["region"]; got != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", got)
	}
	if got := (*body)["limit"]; got != float64(100) {
		t.Errorf("limit = %v, want 100", got)
	}
	if got := (*body)["index"]; got != "physical_index:my_index" {
		t.Errorf("index = %v, want physical_index:my_index", got)
	}
	if _, present := (*body)["direction"]; present {
		t.Error("direction must be omitted so the server applies its own default")
	}
}

func TestMakeLogSearchAPI_OmitsUnsetOptionalFields(t *testing.T) {
	srv, body, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "aggregate"}},
		StartMs:  0,
		EndMs:    60_000,
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	// A limit on an aggregate query is a 400 upstream, so an unset limit must
	// not reach the wire as 0.
	if _, present := (*body)["limit"]; present {
		t.Error("limit must be omitted when unset")
	}
	if _, present := (*body)["index"]; present {
		t.Error("index must be omitted when empty")
	}
}

func TestMakeLogSearchAPI_ClampsLimitToServerMax(t *testing.T) {
	srv, body, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
		Limit:    10_000,
	})
	if err != nil {
		t.Fatalf("MakeLogSearchAPI: %v", err)
	}
	defer resp.Body.Close()

	if got := (*body)["limit"]; got != float64(LogSearchMaxSampleSize) {
		t.Errorf("limit = %v, want %d", got, LogSearchMaxSampleSize)
	}
}

func TestMakeLogSearchAPI_RejectsUnprefixedIndex(t *testing.T) {
	srv, _, _ := captureLogSearchBody(t)
	cfg := logSearchTestCfg(t, srv.URL)

	_, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
		Index:    "my_index",
	})
	if err == nil {
		t.Fatal("a bare index name must be rejected before the request is sent")
	}
}

func TestMakeLogSearchAPI_ReturnsNon200ForCallerToSanitize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"limit is not applicable to an aggregate or dataframe query"}`))
	}))
	defer srv.Close()
	cfg := logSearchTestCfg(t, srv.URL)

	resp, err := MakeLogSearchAPI(context.Background(), srv.Client(), cfg, LogSearchRequest{
		Pipeline: []map[string]interface{}{{"type": "filter"}},
		StartMs:  0,
		EndMs:    60_000,
	})
	if err != nil {
		t.Fatalf("400 must return the response so the caller can sanitize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func decodeLogSearchBody(t *testing.T, payload string) map[string]any {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	out, err := DecodeLogSearchResponse(resp)
	if err != nil {
		t.Fatalf("DecodeLogSearchResponse: %v", err)
	}
	return out
}

func TestDecodeLogSearchResponse_LiftsQueryResultToTopLevel(t *testing.T) {
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[]}]}},
		"total_matching_lines": 42,
		"logs_truncated": true,
		"search_stats": {"bucket_seconds": 60, "chunks_planned": 3}
	}`)

	// data.result must sit exactly where the chunked path leaves it, so nothing
	// downstream can tell which path ran.
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %#v", out["data"])
	}
	if data["resultType"] != "streams" {
		t.Errorf("resultType = %v, want streams", data["resultType"])
	}
	if result, ok := data["result"].([]any); !ok || len(result) != 1 {
		t.Errorf("data.result = %#v, want 1 element", data["result"])
	}
	if _, present := out["query_result"]; present {
		t.Error("query_result must be lifted, not nested")
	}
	if out["total_matching_lines"] != 42 {
		t.Errorf("total_matching_lines = %v, want 42", out["total_matching_lines"])
	}
	if out["logs_truncated"] != true {
		t.Errorf("logs_truncated = %v, want true", out["logs_truncated"])
	}
	if _, present := out["search_stats"]; !present {
		t.Error("search_stats must pass through")
	}
}

func TestDecodeLogSearchResponse_SummarizesVolume(t *testing.T) {
	// Two series over the same three buckets. Counts are strings, in both the
	// integer and the float spelling ClickHouse produces.
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[]}},
		"total_matching_lines": 60,
		"search_stats": {"bucket_seconds": 60},
		"volume": [
			{"metric":{"a":"1"},"values":[[100,"5"],[160,"20"],[220,"0"]]},
			{"metric":{"a":"2"},"values":[[100,"5.000000"],[160,"30"],[220,"0"]]}
		]
	}`)

	summary, ok := out["volume_summary"].(map[string]any)
	if !ok {
		t.Fatalf("volume_summary missing or wrong type: %#v", out["volume_summary"])
	}
	// Bucket 220 sums to zero across both series and is not "with data".
	if summary["buckets_with_data"] != 2 {
		t.Errorf("buckets_with_data = %v, want 2", summary["buckets_with_data"])
	}

	densest, ok := summary["densest"].([]map[string]any)
	if !ok {
		t.Fatalf("densest missing or wrong type: %#v", summary["densest"])
	}
	if len(densest) != 2 {
		t.Fatalf("densest has %d entries, want 2", len(densest))
	}
	// Series are summed per bucket, and the densest comes first.
	if densest[0]["start"] != int64(160) || densest[0]["count"] != float64(50) {
		t.Errorf("densest[0] = %#v, want start 160 count 50", densest[0])
	}
	// end is derived from search_stats.bucket_seconds, not duplicated into the summary.
	if densest[0]["end"] != int64(220) {
		t.Errorf("densest[0].end = %v, want 220", densest[0]["end"])
	}
	if densest[1]["start"] != int64(100) || densest[1]["count"] != float64(10) {
		t.Errorf("densest[1] = %#v, want start 100 count 10", densest[1])
	}
	if _, present := summary["bucket_seconds"]; present {
		t.Error("bucket_seconds belongs to search_stats and must not be duplicated")
	}
}

func TestDecodeLogSearchResponse_KeepsDensestFive(t *testing.T) {
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[]}},
		"search_stats": {"bucket_seconds": 60},
		"volume": [{"metric":{},"values":[
			[100,"1"],[160,"2"],[220,"3"],[280,"4"],[340,"5"],[400,"6"],[460,"7"]
		]}]
	}`)

	summary := out["volume_summary"].(map[string]any)
	densest := summary["densest"].([]map[string]any)
	if len(densest) != 5 {
		t.Fatalf("densest has %d entries, want 5", len(densest))
	}
	if densest[0]["count"] != float64(7) {
		t.Errorf("densest[0].count = %v, want 7", densest[0]["count"])
	}
	if summary["buckets_with_data"] != 7 {
		t.Errorf("buckets_with_data = %v, want 7", summary["buckets_with_data"])
	}
}

func TestDecodeLogSearchResponse_NumericVolumeCountStillReturnsLogLines(t *testing.T) {
	// A count arriving as a bare JSON number instead of the usual stringified
	// float must not sink the search: the log lines are the product, volume is
	// advisory.
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[]}]}},
		"volume": [{"metric":{"a":"1"},"values":[[100,412]]}]
	}`)

	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %#v", out["data"])
	}
	if result, ok := data["result"].([]any); !ok || len(result) != 1 {
		t.Errorf("data.result = %#v, want 1 element", data["result"])
	}
	// A numeric count is now tolerated by logBucketCount.UnmarshalJSON, so the
	// series parses cleanly and volume_summary is present.
	summary, ok := out["volume_summary"].(map[string]any)
	if !ok {
		t.Fatalf("volume_summary missing or wrong type: %#v", out["volume_summary"])
	}
	if summary["buckets_with_data"] != 1 {
		t.Errorf("buckets_with_data = %v, want 1", summary["buckets_with_data"])
	}
}

func TestDecodeLogSearchResponse_MalformedVolumeDropsSummaryButKeepsLogLines(t *testing.T) {
	cases := map[string]string{
		"bucket pair with three elements": `{
			"query_result": {"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[]}]}},
			"volume": [{"metric":{"a":"1"},"values":[[100,"5","extra"]]}]
		}`,
		"volume not an array": `{
			"query_result": {"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[]}]}},
			"volume": {"not":"an array"}
		}`,
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			out := decodeLogSearchBody(t, payload)

			data, ok := out["data"].(map[string]any)
			if !ok {
				t.Fatalf("data missing or wrong type: %#v", out["data"])
			}
			if result, ok := data["result"].([]any); !ok || len(result) != 1 {
				t.Errorf("data.result = %#v, want 1 element", data["result"])
			}
			if _, present := out["volume_summary"]; present {
				t.Error("volume_summary must be absent when volume is structurally malformed")
			}
		})
	}
}

func TestDecodeLogSearchResponse_FloatTotalMatchingLinesStillReturnsLogLines(t *testing.T) {
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[]}]}},
		"total_matching_lines": 42.0
	}`)

	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %#v", out["data"])
	}
	if result, ok := data["result"].([]any); !ok || len(result) != 1 {
		t.Errorf("data.result = %#v, want 1 element", data["result"])
	}
	if out["total_matching_lines"] != 42 {
		t.Errorf("total_matching_lines = %v, want 42", out["total_matching_lines"])
	}
}

func TestDecodeLogSearchResponse_FloatTimestampAndNumericCountParse(t *testing.T) {
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"streams","result":[]}},
		"search_stats": {"bucket_seconds": 60},
		"volume": [{"metric":{"a":"1"},"values":[[100.5,412]]}]
	}`)

	summary, ok := out["volume_summary"].(map[string]any)
	if !ok {
		t.Fatalf("volume_summary missing or wrong type: %#v", out["volume_summary"])
	}
	densest, ok := summary["densest"].([]map[string]any)
	if !ok || len(densest) != 1 {
		t.Fatalf("densest = %#v, want 1 entry", summary["densest"])
	}
	if densest[0]["start"] != int64(100) {
		t.Errorf("start = %v, want 100 (float timestamp truncated)", densest[0]["start"])
	}
	if densest[0]["count"] != float64(412) {
		t.Errorf("count = %v, want 412 (numeric count parsed)", densest[0]["count"])
	}
}

func TestDecodeLogSearchResponse_AggregateOmitsExtras(t *testing.T) {
	// An aggregate query gets no volume, no total, no truncation flag. Their
	// absence is meaningful and must survive.
	out := decodeLogSearchBody(t, `{
		"query_result": {"status":"success","data":{"resultType":"matrix","result":[{"metric":{"count":7},"values":[]}]}},
		"search_stats": {"strategy":"direct"}
	}`)

	for _, key := range []string{"volume_summary", "total_matching_lines", "logs_truncated"} {
		if _, present := out[key]; present {
			t.Errorf("%s must be absent for an aggregate query", key)
		}
	}
	data := out["data"].(map[string]any)
	if data["resultType"] != "matrix" {
		t.Errorf("resultType = %v, want matrix", data["resultType"])
	}
}
