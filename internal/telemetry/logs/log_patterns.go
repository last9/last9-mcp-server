package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// Log pattern mining is a Drain-style template miner run in the MCP server
// over a bounded sample of lines: the Last9 API has no log-pattern endpoint.
const (
	patternWildcard        = "<*>"
	emptyPatternTemplate   = "(empty message)"
	patternRouteTokens     = 2
	patternSimilarity      = 0.5
	maxPatternSampleRunes  = 500
	unknownPatternSeverity = "unknown"
)

// Masks run in order: the specific shapes first, so a bare-number mask cannot
// split a UUID, timestamp or address into several wildcards.
var patternMasks = []*regexp.Regexp{
	regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`),
	regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`),
	regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b`),
	regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`),
	regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`),
	regexp.MustCompile(`\b\d+(\.\d+)?\b`),
}

var patternJSONMessageKeys = []string{"message", "msg", "log", "event", "error", "err"}

// LogPatternStats is one pattern's footprint in one sampled window.
type LogPatternStats struct {
	Count          int            `json:"count"`
	FirstSeen      string         `json:"first_seen,omitempty"`
	LastSeen       string         `json:"last_seen,omitempty"`
	SeverityCounts map[string]int `json:"severity_counts,omitempty"`
}

type logPattern struct {
	tokens  []string
	sample  string
	windows []patternWindow
}

type patternWindow struct {
	count      int
	first      time.Time
	last       time.Time
	severities map[string]int
}

func (p *logPattern) template() string {
	if len(p.tokens) == 0 {
		return emptyPatternTemplate
	}
	return strings.Join(p.tokens, " ")
}

func (p *logPattern) stats(window int) LogPatternStats {
	w := p.windows[window]
	stats := LogPatternStats{Count: w.count}
	if !w.first.IsZero() {
		stats.FirstSeen = w.first.Format(time.RFC3339)
		stats.LastSeen = w.last.Format(time.RFC3339)
	}
	if len(w.severities) > 0 {
		stats.SeverityCounts = w.severities
	}
	return stats
}

// logPatternMiner clusters lines from one or more windows into shared
// templates, so the same template can be compared across windows.
type logPatternMiner struct {
	windows  int
	groups   map[string][]*logPattern
	patterns []*logPattern
}

func newLogPatternMiner(windows int) *logPatternMiner {
	return &logPatternMiner{windows: windows, groups: map[string][]*logPattern{}}
}

func (m *logPatternMiner) add(window int, entry LogEntry) {
	tokens := patternTokens(entry.Message)
	key := patternRouteKey(tokens)
	pattern := bestPatternMatch(m.groups[key], tokens)
	if pattern == nil {
		pattern = &logPattern{
			tokens:  tokens,
			sample:  truncatePatternSample(entry.Message),
			windows: make([]patternWindow, m.windows),
		}
		m.groups[key] = append(m.groups[key], pattern)
		m.patterns = append(m.patterns, pattern)
	} else {
		pattern.generalise(tokens)
	}
	pattern.windows[window].record(entry)
}

func (w *patternWindow) record(entry LogEntry) {
	w.count++
	if ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
		ts = ts.UTC()
		if w.first.IsZero() || ts.Before(w.first) {
			w.first = ts
		}
		if ts.After(w.last) {
			w.last = ts
		}
	}
	severity := strings.TrimSpace(entry.Severity)
	if severity == "" {
		severity = unknownPatternSeverity
	}
	if w.severities == nil {
		w.severities = map[string]int{}
	}
	w.severities[severity]++
}

func (p *logPattern) generalise(tokens []string) {
	for i, token := range tokens {
		if p.tokens[i] != token {
			p.tokens[i] = patternWildcard
		}
	}
}

func bestPatternMatch(candidates []*logPattern, tokens []string) *logPattern {
	var best *logPattern
	bestScore := -1.0
	for _, candidate := range candidates {
		score := patternSimilarityScore(candidate.tokens, tokens)
		if score >= patternSimilarity && score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func patternSimilarityScore(template, tokens []string) float64 {
	if len(template) != len(tokens) {
		return 0
	}
	if len(tokens) == 0 {
		return 1
	}
	same := 0
	for i := range tokens {
		if template[i] == tokens[i] {
			same++
		}
	}
	return float64(same) / float64(len(tokens))
}

// patternRouteKey groups candidates by token count and leading tokens, as
// Drain's fixed-depth tree does. Tokens holding digits route as wildcards
// because they are likely variables the masks missed.
func patternRouteKey(tokens []string) string {
	parts := []string{fmt.Sprint(len(tokens))}
	for i := 0; i < patternRouteTokens && i < len(tokens); i++ {
		token := tokens[i]
		if strings.ContainsAny(token, "0123456789") {
			token = patternWildcard
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, "\x00")
}

func patternTokens(message string) []string {
	text := patternText(message)
	for _, mask := range patternMasks {
		text = mask.ReplaceAllString(text, patternWildcard)
	}
	return strings.Fields(text)
}

// patternText clusters a JSON body on its message field: its other keys
// (timestamps, ids, levels) would otherwise outvote a differing message.
func patternText(message string) string {
	trimmed := strings.TrimSpace(message)
	if !strings.HasPrefix(trimmed, "{") {
		return message
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(trimmed), &body); err != nil {
		return message
	}
	for _, key := range patternJSONMessageKeys {
		if text, ok := body[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return message
}

// LogPatternScopeArgs selects the lines a pattern tool clusters. Both pattern
// tools embed it, so their scope parameters cannot drift apart.
type LogPatternScopeArgs struct {
	ServiceName     string   `json:"service_name" jsonschema:"(Required) Service whose logs to cluster (e.g. checkout)"`
	Env             string   `json:"env,omitempty" jsonschema:"Environment to filter by (e.g. production). Omit when unknown."`
	StartTimeISO    string   `json:"start_time_iso,omitempty" jsonschema:"Window start, RFC3339 (e.g. 2026-10-16T10:00:00Z). If omitted, lookback_minutes is used."`
	EndTimeISO      string   `json:"end_time_iso,omitempty" jsonschema:"Window end, RFC3339. Defaults to now."`
	LookbackMinutes int      `json:"lookback_minutes,omitempty" jsonschema:"Window length in minutes when start_time_iso is omitted (default: 60, minimum: 1)"`
	SeverityFilters []string `json:"severity_filters,omitempty" jsonschema:"Severities to keep, OR logic, case-insensitive (e.g. [error warn]). Omit to cluster every severity."`
	BodyFilters     []string `json:"body_filters,omitempty" jsonschema:"Message substrings to keep, OR logic, case-insensitive (e.g. [timeout])"`
	Index           string   `json:"index,omitempty" jsonschema:"Optional log index, physical_index:<name> or rehydration_index:<block_name>, as discovered by get_service_profile. Omit for the default index."`
	SampleSize      int      `json:"sample_size,omitempty" jsonschema:"Maximum log lines sampled per window (default: 1000, maximum: 5000). Counts are within the sample."`
}

const (
	defaultPatternSampleSize = 1000
	maxPatternSampleSize     = utils.LogSearchMaxSampleSize
	defaultPatternLookback   = 60
	defaultPatternTopN       = 20
	maxPatternTopN           = 100
)

// LogPatternWindow describes the sample one window's patterns were mined from.
type LogPatternWindow struct {
	StartTime          string `json:"start_time"`
	EndTime            string `json:"end_time"`
	SampledLines       int    `json:"sampled_lines"`
	SampleLimitReached bool   `json:"sample_limit_reached"`
	TotalMatchingLines *int   `json:"total_matching_lines,omitempty"`
}

type logPatternScope struct {
	service    string
	env        string
	query      []map[string]interface{}
	index      string
	sampleSize int
}

func newLogPatternScope(args LogPatternScopeArgs) (logPatternScope, error) {
	service := strings.TrimSpace(args.ServiceName)
	if service == "" {
		return logPatternScope{}, fmt.Errorf("service_name is required")
	}
	sampleSize, err := boundedPatternLimit("sample_size", args.SampleSize, defaultPatternSampleSize, maxPatternSampleSize)
	if err != nil {
		return logPatternScope{}, err
	}
	index, err := utils.NormalizeLogIndex(args.Index)
	if err != nil {
		return logPatternScope{}, fmt.Errorf("invalid index: %w", err)
	}
	env := strings.TrimSpace(args.Env)
	query := buildServiceLogsQuery(service, args.SeverityFilters, args.BodyFilters)
	query = addServiceLogsEnvFilter(query, env)
	return logPatternScope{service: service, env: env, query: query, index: index, sampleSize: sampleSize}, nil
}

func (args LogPatternScopeArgs) timeRange() (time.Time, time.Time, error) {
	params := map[string]interface{}{}
	if args.StartTimeISO != "" {
		params["start_time_iso"] = args.StartTimeISO
	}
	if args.EndTimeISO != "" {
		params["end_time_iso"] = args.EndTimeISO
	}
	if args.LookbackMinutes != 0 {
		params["lookback_minutes"] = args.LookbackMinutes
	}
	start, end, err := utils.GetTimeRange(params, defaultPatternLookback)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: %w", err)
	}
	return start, end, nil
}

func (s logPatternScope) sample(ctx context.Context, client *http.Client, cfg models.Config, start, end time.Time) ([]LogEntry, LogPatternWindow, error) {
	logs, err := fetchServiceLogs(ctx, client, cfg, s.service, start, end, s.sampleSize, cloneLogJSONQuery(s.query), s.index)
	if err != nil {
		return nil, LogPatternWindow{}, fmt.Errorf("failed to sample logs: %w", err)
	}
	return logs.Logs, LogPatternWindow{
		StartTime:          start.Format(time.RFC3339),
		EndTime:            end.Format(time.RFC3339),
		SampledLines:       len(logs.Logs),
		SampleLimitReached: len(logs.Logs) >= s.sampleSize,
		TotalMatchingLines: logs.TotalMatchingLines,
	}, nil
}

func boundedPatternLimit(name string, value, fallback, max int) (int, error) {
	switch {
	case value == 0:
		return fallback, nil
	case value < 0 || value > max:
		return 0, fmt.Errorf("%s must be between 1 and %d, got %d", name, max, value)
	default:
		return value, nil
	}
}

func truncatePatternSample(message string) string {
	runes := []rune(message)
	if len(runes) <= maxPatternSampleRunes {
		return message
	}
	return string(runes[:maxPatternSampleRunes]) + "…"
}
