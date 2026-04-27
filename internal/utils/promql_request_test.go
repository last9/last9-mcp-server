package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

// Regression test for the PromQL range/labels/label-values "timestamp anchor"
// bug.
//
// The Last9 PromQL HTTP endpoint interprets the JSON `timestamp` field as the
// END of the query window and runs Prometheus over [timestamp - window,
// timestamp] (this is also how MakePromInstantAPIQuery uses endTimeParam as
// `timestamp`). Sending startTimeParam as `timestamp` therefore shifted every
// range query backwards by exactly one window length.
//
// These tests pin the contract: the marshalled body must carry
// timestamp = endTimeParam and window = endTimeParam - startTimeParam.

const (
	testStartUnix int64 = 1_700_000_000
	testEndUnix   int64 = 1_700_003_600 // start + 1h
)

func newCapturingServer(t *testing.T, capture *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		*capture = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
}

// stubTokenManagerCfg builds a Config whose TokenManager has a pre-populated
// access token so we never hit the real refresh endpoint.
func stubTokenManagerCfg(t *testing.T, apiBaseURL string) models.Config {
	t.Helper()
	tm := &auth.TokenManager{
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	return models.Config{
		APIBaseURL:         apiBaseURL,
		PrometheusReadURL:  "https://prom.example/read",
		PrometheusUsername: "u",
		PrometheusPassword: "p",
		TokenManager:       tm,
	}
}

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&got); err != nil {
		t.Fatalf("decode body: %v\nraw=%s", err, string(raw))
	}
	return got
}

func wantWindow() int64 { return testEndUnix - testStartUnix }

func assertRightAnchored(t *testing.T, body map[string]any) {
	t.Helper()
	ts, ok := body["timestamp"].(float64)
	if !ok {
		t.Fatalf("timestamp missing or wrong type: %v", body["timestamp"])
	}
	if int64(ts) != testEndUnix {
		t.Errorf("timestamp = %d, want %d (= endTimeParam). "+
			"Sending startTimeParam here shifts the returned window backwards "+
			"by one window length.", int64(ts), testEndUnix)
	}
	win, ok := body["window"].(float64)
	if !ok {
		t.Fatalf("window missing or wrong type: %v", body["window"])
	}
	if int64(win) != wantWindow() {
		t.Errorf("window = %d, want %d (= endTimeParam - startTimeParam)",
			int64(win), wantWindow())
	}
}

func TestMakePromRangeAPIQuery_AnchorsOnEndTime(t *testing.T) {
	var captured []byte
	srv := newCapturingServer(t, &captured)
	defer srv.Close()

	cfg := stubTokenManagerCfg(t, srv.URL)
	resp, err := MakePromRangeAPIQuery(
		context.Background(), srv.Client(), "up", testStartUnix, testEndUnix, cfg,
	)
	if err != nil {
		t.Fatalf("MakePromRangeAPIQuery: %v", err)
	}
	defer resp.Body.Close()

	assertRightAnchored(t, decodeBody(t, captured))
}

func TestMakePromLabelValuesAPIQuery_AnchorsOnEndTime(t *testing.T) {
	var captured []byte
	srv := newCapturingServer(t, &captured)
	defer srv.Close()

	cfg := stubTokenManagerCfg(t, srv.URL)
	resp, err := MakePromLabelValuesAPIQuery(
		context.Background(), srv.Client(), "instance", "up",
		testStartUnix, testEndUnix, cfg,
	)
	if err != nil {
		t.Fatalf("MakePromLabelValuesAPIQuery: %v", err)
	}
	defer resp.Body.Close()

	assertRightAnchored(t, decodeBody(t, captured))
}

func TestMakePromLabelsAPIQuery_AnchorsOnEndTime(t *testing.T) {
	var captured []byte
	srv := newCapturingServer(t, &captured)
	defer srv.Close()

	cfg := stubTokenManagerCfg(t, srv.URL)
	resp, err := MakePromLabelsAPIQuery(
		context.Background(), srv.Client(), "up",
		testStartUnix, testEndUnix, cfg,
	)
	if err != nil {
		t.Fatalf("MakePromLabelsAPIQuery: %v", err)
	}
	defer resp.Body.Close()

	assertRightAnchored(t, decodeBody(t, captured))
}
