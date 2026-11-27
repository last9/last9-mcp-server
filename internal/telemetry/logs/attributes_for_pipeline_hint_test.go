package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runBodyHint executes a body-derived hint against sample lines the way the
// logs backend applies it, failing unless the hint is accepted as returned and
// its parse stage yields a value for the filtered attribute on at least one line.
func runBodyHint(t *testing.T, attr LogAttribute, lines []string) {
	t.Helper()
	var stages []map[string]interface{}
	if err := json.Unmarshal([]byte(attr.Hint), &stages); err != nil {
		t.Fatalf("%s: hint is not a JSON pipeline: %v; hint: %s", attr.Name, err, attr.Hint)
	}
	if _, err := prepareLogJSONQuery(stages, "hint"); err != nil {
		t.Fatalf("%s: hint rejected by pipeline validation: %v; hint: %s", attr.Name, err, attr.Hint)
	}
	if len(stages) != 2 || stages[0]["type"] != "parse" || stages[1]["type"] != "filter" {
		t.Fatalf("%s: want parse then filter stages, got: %s", attr.Name, attr.Hint)
	}
	if !strings.Contains(attr.Hint, attr.FilterField) {
		t.Errorf("%s: filter stage does not use filter_field %s; hint: %s", attr.Name, attr.FilterField, attr.Hint)
	}

	parse := stages[0]
	parser, _ := parse["parser"].(string)
	switch parser {
	case "json", "logfmt", "regexp":
	default:
		t.Fatalf("%s: unknown parser in hint: %s", attr.Name, attr.Hint)
	}
	if parser == "json" || parser == "logfmt" {
		labels, _ := parse["labels"].(map[string]interface{})
		if labels[attr.Name] != attr.Name {
			t.Fatalf("%s: %s parse stage must map labels {%q: %q} or the backend rejects it; hint: %s", attr.Name, parser, attr.Name, attr.Name, attr.Hint)
		}
	}

	for _, line := range lines {
		if hintParseValue(parse, attr.Name, line) != "" {
			return
		}
	}
	t.Errorf("%s: hint yields no value on any sample line; hint: %s", attr.Name, attr.Hint)
}

func hintParseValue(parse map[string]interface{}, key, line string) string {
	switch parse["parser"] {
	case "json":
		var obj map[string]interface{}
		if json.Unmarshal([]byte(line), &obj) != nil || obj[key] == nil {
			return ""
		}
		return fmt.Sprint(obj[key])
	case "logfmt":
		for _, m := range regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)=("(?:[^"\\]|\\.)*"|\S+)`).FindAllStringSubmatch(line, -1) {
			if m[1] == key {
				return m[2]
			}
		}
		return ""
	case "regexp":
		pattern, _ := parse["pattern"].(string)
		re := regexp.MustCompile(pattern)
		m := re.FindStringSubmatch(line)
		if m == nil {
			return ""
		}
		return m[re.SubexpIndex(key)]
	}
	return ""
}

func getBodyAttributes(t *testing.T, samples []string) *mcp.CallToolResult {
	t.Helper()
	server := bodySamplingServer(t, `{"status":"success","data":[{"service":"svc"}]}`, samples, nil)
	t.Cleanup(server.Close)
	handler := NewGetLogAttributesForPipelineHandler(server.Client(), testAttrConfig(server.URL))
	res, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesForPipelineArgs{
		Pipeline: []map[string]interface{}{
			{"type": "filter", "query": map[string]interface{}{"$eq": []interface{}{"ServiceName", "svc"}}},
		},
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

// rawAttribute decodes entries as JSON objects so tests see exactly which
// optional keys are served.
func rawAttribute(t *testing.T, res *mcp.CallToolResult, name string) map[string]interface{} {
	t.Helper()
	var discovery struct {
		Attributes []map[string]interface{} `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &discovery); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range discovery.Attributes {
		if e["name"] == name {
			return e
		}
	}
	t.Fatalf("expected attribute %q, got: %v", name, discovery.Attributes)
	return nil
}

func TestGetLogAttributesForPipeline_EveryBodyHintRunsOnSamples(t *testing.T) {
	cases := map[string][]string{
		"json": {
			`{"order_id":"o-1","state":"paid"}`,
			`{"order_id":"o-2","retry":true}`,
		},
		"logfmt": {
			`level=info msg="started job" job_id=42`,
			`level=warn msg="slow job" job_id=43 took=1.5s`,
		},
		"plaintext level": {
			`2026-11-24 10:00:00.123 INFO  worker started`,
			`2026-11-24 10:00:01.456 ERROR worker crashed`,
		},
		"mixed without json": {
			`user=alice action=login`,
			`<event><user>bob</user></event>`,
			`plain text line without structure`,
			`<event><user>carol</user></event>`,
			`another plain line`,
		},
	}
	for name, samples := range cases {
		t.Run(name, func(t *testing.T) {
			ran := 0
			for _, attr := range decodeLogAttributes(t, getBodyAttributes(t, samples)) {
				if attr.Source != "body" || attr.FilterField == "Body" || attr.Hint == "" {
					continue
				}
				runBodyHint(t, attr, samples)
				ran++
			}
			if ran == 0 {
				t.Fatal("fixture produced no body-derived hint to run")
			}
		})
	}
}

func TestGetLogAttributesForPipeline_SparseMixedFormatKeyMarkedLowConfidence(t *testing.T) {
	cases := []struct {
		name        string
		samples     []string
		sparseKey   string
		wantFormats map[string]interface{}
	}{
		{
			name: "key=value, xml and json",
			samples: []string{
				`user=alice action=login status=ok`,
				`<event><user>bob</user></event>`,
				`{"order_id":"o-1","state":"paid"}`,
				`plain text line without structure`,
				`<event><user>carol</user></event>`,
			},
			sparseKey:   "order_id",
			wantFormats: map[string]interface{}{"json": 1.0, "logfmt": 1.0, "text": 3.0},
		},
		{
			name: "key=value and xml only",
			samples: []string{
				`user=alice action=login status=ok`,
				`<event><user>bob</user></event>`,
				`plain text line without structure`,
				`<event><user>carol</user></event>`,
				`another plain line`,
			},
			sparseKey:   "user",
			wantFormats: map[string]interface{}{"logfmt": 1.0, "text": 4.0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := rawAttribute(t, getBodyAttributes(t, tc.samples), tc.sparseKey)
			if entry["sample_coverage"] != "1/5" {
				t.Errorf("sample_coverage = %v, want 1/5", entry["sample_coverage"])
			}
			if entry["confidence"] != "low" {
				t.Errorf("confidence = %v, want low for a key in 1 of 5 samples; entry: %v", entry["confidence"], entry)
			}
			if !reflect.DeepEqual(entry["sample_formats"], tc.wantFormats) {
				t.Errorf("sample_formats = %v, want %v", entry["sample_formats"], tc.wantFormats)
			}
		})
	}
}

func TestGetLogAttributesForPipeline_ConfidenceThreshold(t *testing.T) {
	cases := []struct {
		name      string
		samples   []string
		key       string
		wantLow   bool
		wantShape bool
	}{
		{"2 of 5 is low", []string{`a=1 b=2`, `a=3 b=4`, `x`, `y`, `z`}, "a", true, true},
		{"half is not low", []string{`a=1 b=2`, `plain`}, "a", false, false},
		{"3 of 5 is not low", []string{`a=1 b=2`, `a=3 b=4`, `a=5 b=6`, `y`, `z`}, "a", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := rawAttribute(t, getBodyAttributes(t, tc.samples), tc.key)
			if got := entry["confidence"] == "low"; got != tc.wantLow {
				t.Errorf("confidence = %v, want low=%v (coverage %v)", entry["confidence"], tc.wantLow, entry["sample_coverage"])
			}
			if _, got := entry["sample_formats"]; got != tc.wantShape {
				t.Errorf("sample_formats = %v, want present=%v", entry["sample_formats"], tc.wantShape)
			}
		})
	}
}
