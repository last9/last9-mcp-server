package otelids

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTraceID(t *testing.T) {
	valid := "ea8148dece205073096e4ad48145b08a"
	tests := []struct {
		name     string
		in       string
		want     string
		category string
	}{
		{name: "valid lowercase", in: valid, want: valid},
		{name: "valid uppercase normalized", in: strings.ToUpper(valid), want: valid},
		{name: "surrounding whitespace", in: "  " + valid + "\n", want: valid},
		{name: "empty", in: "", category: CategoryInvalidTraceID},
		{name: "short", in: "abc123", category: CategoryInvalidTraceID},
		{name: "long", in: valid + "00", category: CategoryInvalidTraceID},
		{name: "non-hex", in: "ea8148dece205073096e4ad48145b0zz", category: CategoryInvalidTraceID},
		{name: "all-zero", in: strings.Repeat("0", 32), category: CategoryAllZeroID},
		{name: "16-hex span id", in: "0123456789abcdef", category: CategorySpanIDAsTraceID},
		{name: "16-hex uppercase span id", in: "0123456789ABCDEF", category: CategorySpanIDAsTraceID},
		{name: "16-zero as span id", in: strings.Repeat("0", 16), category: CategorySpanIDAsTraceID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeTraceID(tt.in)
			if tt.category == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if got != "" {
				t.Fatalf("normalized value on error: %q", got)
			}
			var v *Error
			if !errors.As(err, &v) || v.Category != tt.category {
				t.Fatalf("category=%v err=%v", v, err)
			}
			if tt.category == CategorySpanIDAsTraceID && !strings.Contains(err.Error(), "received a span ID where a trace ID is required") {
				t.Fatalf("missing span-id message: %v", err)
			}
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(strings.TrimSpace(tt.in))) && len(strings.TrimSpace(tt.in)) >= 8 {
				t.Fatalf("error must not record the identifier: %v", err)
			}
		})
	}
}

func TestNormalizeSpanID(t *testing.T) {
	valid := "0123456789abcdef"
	tests := []struct {
		name     string
		in       string
		want     string
		category string
	}{
		{name: "valid", in: valid, want: valid},
		{name: "uppercase", in: "0123456789ABCDEF", want: valid},
		{name: "empty", in: "", category: CategoryInvalidSpanID},
		{name: "32-hex", in: "ea8148dece205073096e4ad48145b08a", category: CategoryInvalidSpanID},
		{name: "non-hex", in: "0123456789abcdzz", category: CategoryInvalidSpanID},
		{name: "all-zero", in: strings.Repeat("0", 16), category: CategoryAllZeroID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeSpanID(tt.in)
			if tt.category == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			var v *Error
			if !errors.As(err, &v) || v.Category != tt.category {
				t.Fatalf("category=%v err=%v", v, err)
			}
		})
	}
}
