package metriccatalog

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReceiverLookupFixtures(t *testing.T) {
	raw, err := os.ReadFile("receivers_generated.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]struct{ Type, Unit, Help string }
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, kind, unit string }{
		{"redis_memory_used_bytes", "gauge", "By"},
		{"redis_clients_connected", "gauge", "{client}"},
		{"redis_commands_processed_total", "counter", "{command}"},
		{"system_cpu_time_seconds_total", "counter", "s"},
		{"redis_db_avg_ttl_milliseconds", "gauge", "ms"},
	} {
		got := entries[tt.name]
		if got.Type != tt.kind || got.Unit != tt.unit || got.Help == "" {
			t.Errorf("%s: %+v", tt.name, got)
		}
	}
}

func TestReceiverLookupFresh(t *testing.T) {
	raw, err := os.ReadFile("receiver_sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var sources []ReceiverSource
	if err := json.Unmarshal(raw, &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 22 {
		t.Fatalf("receiver coverage changed: %d", len(sources))
	}
	for _, source := range sources {
		if source.Version != "v0.120.0" || len(source.SHA256) != 64 || len(source.Metrics) == 0 {
			t.Fatalf("unpinned source: %s", source.Path)
		}
	}
	want, err := GenerateReceivers(sources)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("receivers_generated.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("stale lookup: run go run ./internal/metriccatalog/cmd/generate from the repo root")
	}
}

func TestReceiverTranslation(t *testing.T) {
	for _, tt := range []struct {
		metric ReceiverMetric
		want   string
	}{
		{ReceiverMetric{Name: "cpu.time", Kind: "sum", Monotonic: true, Unit: "s"}, "cpu_time_seconds_total"},
		{ReceiverMetric{Name: "connections", Kind: "sum", Unit: "{connection}"}, "connections"},
		{ReceiverMetric{Name: "memory.bytes", Kind: "gauge", Unit: "By"}, "memory_bytes"},
		{ReceiverMetric{Name: "cache.ratio", Kind: "gauge", Unit: "1"}, "cache_ratio"},
		{ReceiverMetric{Name: "ops", Kind: "gauge", Unit: "{operation}/s"}, "ops_per_second"},
		{ReceiverMetric{Name: "calls.total", Kind: "sum", Monotonic: true, Unit: "s"}, "calls_seconds_total"},
		{ReceiverMetric{Name: "load", Kind: "gauge", Unit: "%"}, "load_percent"},
	} {
		if got := translatedName(tt.metric); got != tt.want {
			t.Errorf("%+v: %s != %s", tt.metric, got, tt.want)
		}
	}
}

func TestReceiverCollisionsStayUnknown(t *testing.T) {
	sources := []ReceiverSource{{Metrics: []ReceiverMetric{
		{Name: "same.name", Kind: "gauge", Unit: "s", Help: "A"},
		{Name: "same_name", Kind: "sum", Monotonic: true, Unit: "s", Help: "B"},
		{Name: "same.name", Kind: "gauge", Unit: "s", Help: "A"},
	}}}
	raw, err := GenerateReceivers(sources)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]Definition
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["same_name"]; ok {
		t.Fatal("ambiguous alias was restored")
	}
}
