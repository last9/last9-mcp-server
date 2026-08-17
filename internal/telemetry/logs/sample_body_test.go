package logs

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSampleBodyForModel covers each transform sampleBodyForModel applies,
// individually, plus the untransformed pass-through case.
func TestSampleBodyForModel(t *testing.T) {
	t.Run("multi-line body escapes newlines/tabs without collapsing", func(t *testing.T) {
		line := "line one\n\tline two\r\nline three"
		got, notes := sampleBodyForModel(line)
		if strings.ContainsAny(got, "\n\t\r") {
			t.Errorf("expected no literal newline/tab/CR left in output, got: %q", got)
		}
		if !strings.Contains(got, `\n`) || !strings.Contains(got, `\t`) {
			t.Errorf("expected \\n and \\t escape sequences present, got: %q", got)
		}
		if got != `line one\n\tline two\r\nline three` {
			t.Errorf("expected whitespace runs NOT collapsed, got: %q", got)
		}
		if !containsNote(notes, "multiline-escaped") {
			t.Errorf("expected note multiline-escaped, got: %v", notes)
		}
	})

	t.Run("credential value redacted with key preserved", func(t *testing.T) {
		line := "GET /path?token=abc HTTP/1.1"
		got, notes := sampleBodyForModel(line)
		if !strings.Contains(got, "token=[redacted]") {
			t.Errorf("expected key preserved with redacted value, got: %q", got)
		}
		if strings.Contains(got, "=abc") {
			t.Errorf("expected credential value stripped, got: %q", got)
		}
		if !containsNote(notes, "credential-redacted") {
			t.Errorf("expected note credential-redacted, got: %v", notes)
		}
	})

	t.Run("full URL redacted", func(t *testing.T) {
		line := "outbound call to https://upstream.example.com/v1/charge failed"
		got, notes := sampleBodyForModel(line)
		if !strings.Contains(got, "[redacted-url]") {
			t.Errorf("expected redacted-url marker, got: %q", got)
		}
		if strings.Contains(got, "https://upstream.example.com") {
			t.Errorf("expected original URL stripped, got: %q", got)
		}
		if !containsNote(notes, "url-redacted") {
			t.Errorf("expected note url-redacted, got: %v", notes)
		}
	})

	t.Run("over-limit line truncated with marker and note", func(t *testing.T) {
		line := strings.Repeat("a", 600)
		got, notes := sampleBodyForModel(line)
		if !strings.HasSuffix(got, "(truncated)") {
			t.Errorf("expected truncation marker suffix, got: %q", got)
		}
		if !containsNote(notes, "truncated") {
			t.Errorf("expected note truncated, got: %v", notes)
		}
	})

	t.Run("unicode format rune stripped", func(t *testing.T) {
		line := "normal‮text" // U+202E RIGHT-TO-LEFT OVERRIDE
		got, notes := sampleBodyForModel(line)
		if strings.ContainsRune(got, '‮') {
			t.Errorf("expected format rune stripped, got: %q", got)
		}
		if got != "normaltext" {
			t.Errorf("expected only the format rune removed, got: %q", got)
		}
		if !containsNote(notes, "control-stripped") {
			t.Errorf("expected note control-stripped, got: %v", notes)
		}
	})

	t.Run("clean line unchanged with empty notes", func(t *testing.T) {
		line := "just a plain clean log line with no special content"
		got, notes := sampleBodyForModel(line)
		if got != line {
			t.Errorf("expected clean line unchanged, got: %q, want: %q", got, line)
		}
		if notes == nil {
			t.Error("expected notes to be a non-nil empty slice")
		}
		if len(notes) != 0 {
			t.Errorf("expected no notes for a clean line, got: %v", notes)
		}
	})
}

func containsNote(notes []string, note string) bool {
	for _, n := range notes {
		if n == note {
			return true
		}
	}
	return false
}

// TestGetLogAttributesForPipeline_PlaintextFallbackFiveDistinctSamplesCappedAtThree
// verifies the plaintext fallback entry caps sample_bodies at 3 even when 5
// distinct unstructured lines are sampled — a mixed-format service must not
// flood the response, and the model must see more than one shape.
func TestGetLogAttributesForPipeline_PlaintextFallbackFiveDistinctSamplesCappedAtThree(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{
		`plain request from host-1`,
		`plain request from host-2`,
		`plain request from host-3`,
		`plain request from host-4`,
		`plain request from host-5`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	entry := attrByName(attrs, "body")
	if entry == nil {
		t.Fatalf("expected plaintext body fallback entry, got: %+v", attrs)
	}
	if len(entry.SampleBodies) != 3 {
		t.Fatalf("expected exactly 3 sample_bodies (capped), got %d: %v", len(entry.SampleBodies), entry.SampleBodies)
	}
	seen := map[string]struct{}{}
	for _, s := range entry.SampleBodies {
		if _, dup := seen[s]; dup {
			t.Errorf("expected distinct sample_bodies, got duplicate: %q", s)
		}
		seen[s] = struct{}{}
	}
}

// TestGetLogAttributesForPipeline_PlaintextFallbackNearIdenticalSamplesDeduped
// verifies that when the sampled lines are mostly identical after transform,
// fewer than the cap are returned and no duplicates appear.
func TestGetLogAttributesForPipeline_PlaintextFallbackNearIdenticalSamplesDeduped(t *testing.T) {
	series := `{"status":"success","data":[{"service":"foo-service"}]}`
	samples := []string{
		`connection reset by peer`,
		`connection reset by peer`,
		`connection reset by peer`,
		`connection reset by peer`,
		`a genuinely different line`,
	}
	server := bodySamplingServer(t, series, samples, nil)
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), cfg)
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{{"type": "filter", "query": map[string]interface{}{}}},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	attrs := decodeLogAttributes(t, res)
	entry := attrByName(attrs, "body")
	if entry == nil {
		t.Fatalf("expected plaintext body fallback entry, got: %+v", attrs)
	}
	if len(entry.SampleBodies) != 2 {
		t.Fatalf("expected exactly 2 distinct sample_bodies (4 duplicates + 1 distinct), got %d: %v", len(entry.SampleBodies), entry.SampleBodies)
	}
	if entry.SampleBodies[0] != "connection reset by peer" || entry.SampleBodies[1] != "a genuinely different line" {
		t.Errorf("unexpected sample_bodies content: %v", entry.SampleBodies)
	}
}
