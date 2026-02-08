package knowledge

import (
	"testing"
)

func TestDetectFormat_JSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"json object", `{"service_name":"frontend","incoming":{}}`},
		{"json array", `[{"metric":{"__name__":"up"},"value":[1234,"1"]}]`},
		{"json with whitespace", `  { "key": "value" }  `},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, parsed, err := DetectFormat(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if format != FormatJSON {
				t.Errorf("expected FormatJSON, got %s", format)
			}
			if parsed == nil {
				t.Error("expected parsed value, got nil")
			}
		})
	}
}

func TestDetectFormat_YAML(t *testing.T) {
	input := "name: test\nnodes:\n  - Service\n  - Pod\n"
	format, parsed, err := DetectFormat(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatYAML {
		t.Errorf("expected FormatYAML, got %s", format)
	}
	if parsed == nil {
		t.Error("expected parsed value, got nil")
	}
}

func TestDetectFormat_CSV(t *testing.T) {
	input := "name,type,value\nfoo,Service,1\nbar,Pod,2\nbaz,Container,3\n"
	format, parsed, err := DetectFormat(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatCSV {
		t.Errorf("expected FormatCSV, got %s", format)
	}
	records, ok := parsed.([][]string)
	if !ok {
		t.Fatal("expected [][]string parsed value")
	}
	if len(records) != 4 {
		t.Errorf("expected 4 rows, got %d", len(records))
	}
}

func TestDetectFormat_PlainText(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"log line", "Connection to db:postgres failed"},
		{"prose", "The service is experiencing high latency"},
		{"bare string yaml", "just a simple string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, _, err := DetectFormat(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if format != FormatPlainText {
				t.Errorf("expected FormatPlainText, got %s", format)
			}
		})
	}
}

func TestDetectFormat_Empty(t *testing.T) {
	format, parsed, err := DetectFormat("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatUnknown {
		t.Errorf("expected FormatUnknown, got %s", format)
	}
	if parsed != nil {
		t.Error("expected nil parsed for empty input")
	}
}

func TestDetectFormat_MalformedJSON(t *testing.T) {
	// Starts with { but isn't valid JSON — should fall through to PlainText
	input := `{this is not json at all`
	format, _, err := DetectFormat(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatPlainText {
		t.Errorf("expected FormatPlainText for malformed JSON, got %s", format)
	}
}

func TestDetectFormat_JSONPrioritizedOverYAML(t *testing.T) {
	// Valid JSON is also valid YAML, but JSON should win
	input := `{"key": "value"}`
	format, _, _ := DetectFormat(input)
	if format != FormatJSON {
		t.Errorf("expected FormatJSON (JSON before YAML), got %s", format)
	}
}
