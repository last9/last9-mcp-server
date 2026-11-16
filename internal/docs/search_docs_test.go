package docs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureIndex = `{"generated_at":"2026-11-16T00:00:00Z","pages":[
{"url":"https://last9.io/docs/otel/kubernetes-operator/","title":"Kubernetes Operator","description":"Install telemetry","headings":["Resource attributes"],"sections":[{"heading":"Resource attributes","anchor":"resource-attributes","text":"The OpenTelemetry k8sattributes processor adds Kubernetes metadata to spans and metrics."}]},
{"url":"https://last9.io/docs/logs/kubernetes/","title":"Kubernetes logs","description":"Collect pod logs","headings":["Kubernetes resource attributes"],"sections":[{"heading":"Kubernetes resource attributes","anchor":"attributes","text":"Add pod and namespace resource attributes to Kubernetes logs."}]},
{"url":"https://last9.io/docs/metrics/kubernetes/","title":"Kubernetes metrics","description":"Collect cluster metrics","headings":["Kubernetes resource attributes"],"sections":[{"heading":"Kubernetes resource attributes","anchor":"resource-attributes","text":"Use pod resource attributes when querying Kubernetes metrics."}]},
{"url":"https://last9.io/docs/prometheus/","title":"Prometheus queries","description":"Query metrics","headings":[],"sections":[{"heading":"PromQL","anchor":"promql","text":"Write Prometheus expressions."}]}]}`

func TestSearchRanksKubernetesSynonyms(t *testing.T) {
	f := fixtureFetcher(t, fixtureIndex)
	hits, err := f.Search(context.Background(), "k8s attributes", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"https://last9.io/docs/otel/kubernetes-operator/#resource-attributes": true,
		"https://last9.io/docs/logs/kubernetes/#attributes":                   true,
		"https://last9.io/docs/metrics/kubernetes/#resource-attributes":       true,
	}
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3: %#v", len(hits), hits)
	}
	for _, hit := range hits {
		if !want[hit.URL] {
			t.Errorf("unexpected URL %q", hit.URL)
		}
	}
}

func TestFetcherConditionalRefreshAndStaleFallback(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, fixtureIndex)
		case 2:
			if got := r.Header.Get("If-None-Match"); got != `"v1"` {
				t.Errorf("If-None-Match = %q", got)
			}
			w.WriteHeader(http.StatusNotModified)
		default:
			http.Error(w, "unavailable", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	f := NewFetcher(srv.Client(), srv.URL)
	f.ttl = 0
	for i := 0; i < 3; i++ {
		hits, err := f.Search(context.Background(), "promql", 5)
		if err != nil || len(hits) != 1 {
			t.Fatalf("search %d: hits=%#v err=%v", i, hits, err)
		}
	}
}

func TestFetcherRejectsInitialFailureAndOversizeBody(t *testing.T) {
	tests := []struct {
		name string
		fn   http.HandlerFunc
	}{
		{"initial failure", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadGateway) }},
		{"oversize", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(maxIndexBytes+1))
			w.WriteHeader(http.StatusOK)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.fn)
			defer srv.Close()
			if _, err := NewFetcher(srv.Client(), srv.URL).Search(context.Background(), "logs", 5); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSearchLimitNoMatchAndExcerptBoundary(t *testing.T) {
	long := strings.Repeat("word ", 170) + "ending"
	index := strings.Replace(fixtureIndex, "Write Prometheus expressions.", long, 1)
	f := fixtureFetcher(t, index)
	hits, err := f.Search(context.Background(), "word", 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || len(hits[0].Excerpt) > 800 || strings.HasSuffix(hits[0].Excerpt, " wor") {
		t.Fatalf("bad excerpt: len=%d %q", len(hits[0].Excerpt), hits[0].Excerpt)
	}
	if hits, err = f.Search(context.Background(), "doesnotexist", -2); err != nil || len(hits) != 0 {
		t.Fatalf("no match: hits=%#v err=%v", hits, err)
	}
	if _, err := f.Search(context.Background(), "  ", 5); err == nil {
		t.Fatal("empty query accepted")
	}
}

func fixtureFetcher(t *testing.T, body string) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
	t.Cleanup(srv.Close)
	f := NewFetcher(srv.Client(), srv.URL)
	f.ttl = time.Hour
	return f
}
