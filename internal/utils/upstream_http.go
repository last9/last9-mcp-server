package utils

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

const UpstreamBodyLimit = 512

var (
	upstreamBodyURLPattern    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>,}\]]+`)
	upstreamBodyBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[^\s"',}]+`)
	upstreamBodySecretPattern = regexp.MustCompile(`(?i)\b(token|api[_-]?key|secret|password|authorization)\b"?\s*[:=]\s*"?[^"',}\s]+`)
)

// SanitizeUpstreamBody redacts URLs/credentials, strips controls, and bounds size
// so 400 bodies can be relayed to the model without leaking internals.
func SanitizeUpstreamBody(raw string) string {
	cleaned := upstreamBodyURLPattern.ReplaceAllString(raw, "[redacted-url]")
	cleaned = upstreamBodyBearerPattern.ReplaceAllString(cleaned, "[redacted-credential]")
	cleaned = upstreamBodySecretPattern.ReplaceAllString(cleaned, "[redacted-credential]")
	cleaned = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if len(cleaned) > UpstreamBodyLimit {
		cleaned = strings.ToValidUTF8(cleaned[:UpstreamBodyLimit], "") + "… (truncated)"
	}
	return cleaned
}

// NewUpstreamHTTPError maps a non-OK HTTP response to a tool-facing error.
// 400/422 bodies are sanitized and included. 5xx bodies are drained and omitted.
func NewUpstreamHTTPError(resp *http.Response, op string) error {
	if resp == nil {
		return fmt.Errorf("%s failed: empty upstream response", op)
	}
	status := resp.StatusCode
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		body := readLimitedResponseBody(resp.Body, 4<<10)
		return fmt.Errorf("%s failed with HTTP %d. Review the tool arguments and retry. Upstream response: %s", op, status, body)
	}
	drainResponseBody(resp.Body)
	return fmt.Errorf("%s failed with HTTP %d. %s", op, status, upstreamStatusAdvice(status))
}

func upstreamStatusAdvice(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Check the connection credentials and data access."
	case http.StatusNotFound:
		return "The capability may be unavailable or disabled."
	case http.StatusRequestTimeout:
		return "Retry after a short delay or request a smaller time window."
	case http.StatusTooManyRequests:
		return "Rate limited; retry after a short delay."
	default:
		if statusCode >= http.StatusInternalServerError {
			return "The upstream service is temporarily unavailable; retry later."
		}
		return "Review the tool arguments and retry."
	}
}

func drainResponseBody(body io.Reader) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 4<<10)
}

func readLimitedResponseBody(body io.Reader, limit int64) string {
	if body == nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = io.CopyN(&buf, body, limit)
	_, _ = io.CopyN(io.Discard, body, 4<<10)
	return SanitizeUpstreamBody(buf.String())
}
