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
	tokens    []string
	wildcards int
	sample    string
	sampled   bool
	windows   []patternWindow
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

// mineLogPatterns clusters every window's lines into shared templates and
// returns those holding a line. Counting waits for learning to end: templates
// generalise as lines arrive, so earlier counts would split identical lines.
func mineLogPatterns(windows ...[]LogEntry) ([]*logPattern, error) {
	miner := logPatternMiner{groups: map[string][]*logPattern{}}
	tokens := make([][][]string, len(windows))
	for w, entries := range windows {
		tokens[w] = make([][]string, len(entries))
		for i, entry := range entries {
			tokens[w][i] = patternTokens(entry.Message)
			miner.learn(tokens[w][i])
		}
	}
	if err := miner.count(windows, tokens); err != nil {
		return nil, err
	}

	counted := make([]*logPattern, 0, len(miner.patterns))
	for _, p := range miner.patterns {
		if p.sampled {
			counted = append(counted, p)
		}
	}
	return counted, nil
}

type logPatternMiner struct {
	groups   map[string][]*logPattern
	patterns []*logPattern
}

func (m *logPatternMiner) learn(tokens []string) {
	key := patternRouteKey(tokens)
	if pattern := bestPatternMatch(m.groups[key], tokens); pattern != nil {
		pattern.generalise(tokens)
		return
	}
	pattern := &logPattern{tokens: append([]string(nil), tokens...)}
	for _, token := range tokens {
		if token == patternWildcard {
			pattern.wildcards++
		}
	}
	m.groups[key] = append(m.groups[key], pattern)
	m.patterns = append(m.patterns, pattern)
}

// assign returns the most specific final template the line fits, earliest
// first on ties. One always fits: the template that learned the line only
// ever turned tokens differing from it into wildcards.
func (m *logPatternMiner) assign(tokens []string) *logPattern {
	var best *logPattern
	for _, candidate := range m.groups[patternRouteKey(tokens)] {
		if candidate.fits(tokens) && (best == nil || candidate.wildcards < best.wildcards) {
			best = candidate
		}
	}
	return best
}

func (m *logPatternMiner) count(windows [][]LogEntry, tokens [][][]string) error {
	for _, p := range m.patterns {
		p.windows = make([]patternWindow, len(windows))
	}
	for w, entries := range windows {
		for i, entry := range entries {
			pattern := m.assign(tokens[w][i])
			if pattern == nil {
				return fmt.Errorf("log line %q fits no log pattern, so pattern counts would be wrong", truncatePatternSample(entry.Message))
			}
			pattern.record(w, entry)
		}
	}
	return nil
}

func (p *logPattern) fits(tokens []string) bool {
	if len(p.tokens) != len(tokens) {
		return false
	}
	for i, token := range p.tokens {
		if token != patternWildcard && token != tokens[i] {
			return false
		}
	}
	return true
}

func (p *logPattern) record(window int, entry LogEntry) {
	if !p.sampled {
		p.sample, p.sampled = truncatePatternSample(entry.Message), true
	}
	p.windows[window].record(entry)
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
		if p.tokens[i] != token && p.tokens[i] != patternWildcard {
			p.tokens[i] = patternWildcard
			p.wildcards++
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
