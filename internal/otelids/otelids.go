package otelids

import (
	"fmt"
	"strings"
)

const (
	TraceIDHexLen = 32
	SpanIDHexLen  = 16

	CategoryInvalidTraceID  = "invalid_trace_id"
	CategorySpanIDAsTraceID = "span_id_as_trace_id"
	CategoryInvalidSpanID   = "invalid_span_id"
	CategoryAllZeroID       = "all_zero_id"
)

// Error is a local validation failure. Message never includes the identifier
// so logs and metrics cannot leak customer IDs.
type Error struct {
	Category string
	Message  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (category=%s)", e.Message, e.Category)
}

func isHex(r byte) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func normalizeHex(s string, wantLen int) (string, bool) {
	if len(s) != wantLen {
		return "", false
	}
	out := make([]byte, wantLen)
	allZero := true
	for i := 0; i < wantLen; i++ {
		c := s[i]
		if !isHex(c) {
			return "", false
		}
		if c >= 'A' && c <= 'F' {
			c += 'a' - 'A'
		}
		out[i] = c
		if c != '0' {
			allZero = false
		}
	}
	if allZero {
		return "", false
	}
	return string(out), true
}

func isAllZeroHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

// NormalizeTraceID accepts a 32-character hexadecimal OpenTelemetry trace ID.
// Uppercase hex is folded to lowercase. A 16-hex value is rejected as a span ID
// supplied where a trace ID is required.
func NormalizeTraceID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", &Error{
			Category: CategoryInvalidTraceID,
			Message:  "trace_id is required and must be a 32-character hexadecimal OpenTelemetry trace ID",
		}
	}
	if len(s) == SpanIDHexLen {
		if _, ok := normalizeHex(s, SpanIDHexLen); ok || isAllZeroHex(s) {
			return "", &Error{
				Category: CategorySpanIDAsTraceID,
				Message:  "received a span ID where a trace ID is required",
			}
		}
	}
	if isAllZeroHex(s) && len(s) == TraceIDHexLen {
		return "", &Error{
			Category: CategoryAllZeroID,
			Message:  "trace_id must be a non-zero 32-character hexadecimal OpenTelemetry trace ID",
		}
	}
	normalized, ok := normalizeHex(s, TraceIDHexLen)
	if !ok {
		return "", &Error{
			Category: CategoryInvalidTraceID,
			Message:  "trace_id must be a 32-character hexadecimal OpenTelemetry trace ID",
		}
	}
	return normalized, nil
}

// NormalizeSpanID accepts a 16-character hexadecimal OpenTelemetry span ID.
// Uppercase hex is folded to lowercase.
func NormalizeSpanID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", &Error{
			Category: CategoryInvalidSpanID,
			Message:  "span_id must be a 16-character hexadecimal OpenTelemetry span ID",
		}
	}
	if isAllZeroHex(s) && len(s) == SpanIDHexLen {
		return "", &Error{
			Category: CategoryAllZeroID,
			Message:  "span_id must be a non-zero 16-character hexadecimal OpenTelemetry span ID",
		}
	}
	normalized, ok := normalizeHex(s, SpanIDHexLen)
	if !ok {
		return "", &Error{
			Category: CategoryInvalidSpanID,
			Message:  "span_id must be a 16-character hexadecimal OpenTelemetry span ID",
		}
	}
	return normalized, nil
}
