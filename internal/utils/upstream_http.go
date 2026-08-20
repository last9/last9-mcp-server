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

const (
	UpstreamBodyLimit              = 512
	MetricsTooManySamplesErrorCode = "METRICS_QUERY_TOO_MANY_SAMPLES"
)

var (
	upstreamBodyURLPattern    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>,}\]]+`)
	upstreamBodyBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[^\s"',}]+`)
	upstreamBodySecretPattern = regexp.MustCompile(`(?i)\b(` + CredentialKeyAlternation + `)\b"?\s*[:=]\s*"?[^"',}\s]+`)
)

// CredentialKeyAlternation is shared with the log sample-body sanitizer, which
// needs its own compiled pattern (capture groups preserve the key) but must
// redact the same key names.
const CredentialKeyAlternation = `token|api[_-]?key|secret|password|authorization`

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
// Optional hint is appended after 400/422 bodies (e.g. pipeline schema reminder).
func NewUpstreamHTTPError(resp *http.Response, op string, hint ...string) error {
	if resp == nil {
		return fmt.Errorf("%s failed: empty upstream response", op)
	}
	status := resp.StatusCode
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		body := ReadLimitedResponseBody(resp.Body, 4<<10)
		if status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(body), "too many samples") {
			return fmt.Errorf(
				"%s failed with HTTP %d (%s). The generated metrics query would scan too many samples. Agent action required: do not ask the user to edit PromQL. Retry with narrower filters already known from the request (for example, service, environment, operation, or label). If the full time range is still required, split it into smaller subranges only when the results can be combined correctly; preserve the requested coverage and disclose any limitations. Never average percentile values across subranges. Ask the user to narrow the scope only when the requested result cannot be computed correctly. Do not retry the same query unchanged.",
				op,
				status,
				MetricsTooManySamplesErrorCode,
			)
		}
		msg := fmt.Sprintf("%s failed with HTTP %d. Review the tool arguments and retry. Upstream response: %s", op, status, body)
		if len(hint) > 0 && strings.TrimSpace(hint[0]) != "" {
			msg += "\n\n" + hint[0]
		}
		return fmt.Errorf("%s", msg)
	}
	DrainResponseBody(resp.Body)
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

func DrainResponseBody(body io.Reader) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 4<<10)
}

func ReadLimitedResponseBody(body io.Reader, limit int64) string {
	if body == nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = io.CopyN(&buf, body, limit)
	_, _ = io.CopyN(io.Discard, body, 4<<10)
	return SanitizeUpstreamBody(buf.String())
}
