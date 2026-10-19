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

// unknownFieldMarkers are lowercase fragments of upstream rejections for a query
// that names a field the data does not have, including the query engine's own
// wording that the API passes through.
var unknownFieldMarkers = []string{
	"unknown field",
	"unknown attribute",
	"unknown column",
	"unknown identifier",
	"missing columns",
	"no such column",
}

// UnknownFieldNextAction returns the follow-up for an upstream body that names an
// unknown field, pointing at discoveryTool; "" when the body is about something else.
func UnknownFieldNextAction(body, discoveryTool string) string {
	if discoveryTool == "" {
		return ""
	}
	lower := strings.ToLower(body)
	for _, marker := range unknownFieldMarkers {
		if strings.Contains(lower, marker) {
			return fmt.Sprintf("Next action: call %s with the same scope to list the field names this data has, fix the name, and retry.", discoveryTool)
		}
	}
	return ""
}

// UpstreamAdvice tailors an upstream error to the calling tool.
type UpstreamAdvice struct {
	// SchemaHint is appended after 400/422 bodies (e.g. pipeline schema reminder).
	SchemaHint string
	// DiscoveryTool is named as the next action when the upstream rejects a field.
	DiscoveryTool string
}

// UpstreamHTTPError is the typed form of a non-OK upstream response; use errors.As to
// branch on StatusCode. Body is the sanitized 4xx body (relayed in Error() only for 400/422)
// and empty for 5xx.
type UpstreamHTTPError struct {
	StatusCode int
	Body       string
	message    string
}

func (e *UpstreamHTTPError) Error() string { return e.message }

// NewUpstreamHTTPError maps a non-OK HTTP response to a tool-facing error.
// 400/422 bodies are sanitized and included. 5xx bodies are drained and omitted.
func NewUpstreamHTTPError(resp *http.Response, op string) error {
	return NewUpstreamHTTPErrorWithAdvice(resp, op, UpstreamAdvice{})
}

// NewUpstreamHTTPErrorWithAdvice is NewUpstreamHTTPError with tool-specific advice.
func NewUpstreamHTTPErrorWithAdvice(resp *http.Response, op string, advice UpstreamAdvice) error {
	if resp == nil {
		return fmt.Errorf("%s failed: empty upstream response", op)
	}
	status := resp.StatusCode
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		body := ReadLimitedResponseBody(resp.Body, 4<<10)
		if status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(body), "too many samples") {
			return &UpstreamHTTPError{StatusCode: status, Body: body, message: fmt.Sprintf(
				"%s failed with HTTP %d (%s). The generated metrics query would scan too many samples. Agent action required: do not ask the user to edit PromQL. Retry with narrower filters already known from the request (for example, service, environment, operation, or label). If the full time range is still required, split it into smaller subranges only when the results can be combined correctly; preserve the requested coverage and disclose any limitations. Never average percentile values across subranges. Ask the user to narrow the scope only when the requested result cannot be computed correctly. Do not retry the same query unchanged.",
				op,
				status,
				MetricsTooManySamplesErrorCode,
			)}
		}
		next := UnknownFieldNextAction(body, advice.DiscoveryTool)
		if next == "" {
			next = "Review the tool arguments and retry."
		}
		msg := fmt.Sprintf("%s failed with HTTP %d. %s Upstream response: %s", op, status, next, body)
		if strings.TrimSpace(advice.SchemaHint) != "" {
			msg += "\n\n" + advice.SchemaHint
		}
		return &UpstreamHTTPError{StatusCode: status, Body: body, message: msg}
	}
	var body string
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		body = ReadLimitedResponseBody(resp.Body, 4<<10)
	} else {
		DrainResponseBody(resp.Body)
	}
	return &UpstreamHTTPError{StatusCode: status, Body: body, message: fmt.Sprintf("%s failed with HTTP %d. %s", op, status, upstreamStatusAdvice(status))}
}

// UpstreamAuthAdvice is the next action for a 401 or 403; "" for any other status.
func UpstreamAuthAdvice(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "The Last9 credentials were rejected: regenerate the refresh token under Settings → API Access, or sign in again for OAuth connections. Do not retry unchanged."
	case http.StatusForbidden:
		return "The credentials lack access to this data: the token's scope or the user's role must allow reading it. Ask a Last9 admin to grant that access or issue a token that has it. Do not retry unchanged."
	}
	return ""
}

func upstreamStatusAdvice(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return UpstreamAuthAdvice(statusCode)
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
