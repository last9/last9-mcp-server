package logs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
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

func truncatePatternSample(message string) string {
	runes := []rune(message)
	if len(runes) <= maxPatternSampleRunes {
		return message
	}
	return string(runes[:maxPatternSampleRunes]) + "…"
}
