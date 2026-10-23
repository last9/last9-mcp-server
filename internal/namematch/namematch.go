// Package namematch resolves an approximate name typed by an agent to one of
// a known set of names, refusing to guess when more than one fits.
package namematch

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind int

const (
	None Kind = iota
	Exact
	Closest
	Ambiguous
)

const MaxCandidates = 5

// minFuzzyRunes keeps short inputs ("st", "db") from matching unrelated names.
const minFuzzyRunes = 3

type Match struct {
	Kind       Kind
	Name       string
	Candidates []string
	// Omitted counts ambiguous candidates cut by MaxCandidates.
	Omitted int
}

func Resolve(input string, names []string) Match {
	for _, name := range names {
		if name == input {
			return Match{Kind: Exact, Name: name}
		}
	}
	key := normalize(input)
	if len([]rune(key)) == 0 {
		return Match{}
	}
	rules := []func(string) []string{
		func(key string) []string { return filter(names, func(n string) bool { return normalize(n) == key }) },
		func(key string) []string {
			return filter(names, func(n string) bool { return compact(normalize(n)) == compact(key) })
		},
		func(key string) []string { return nearest(key, names) },
		func(key string) []string {
			if len([]rune(key)) < minFuzzyRunes {
				return nil
			}
			return filter(names, func(n string) bool { return containsWhole(normalize(n), key) })
		},
	}
	for _, rule := range rules {
		if hits := rule(key); len(hits) > 0 {
			return decide(hits)
		}
	}
	return Match{}
}

func decide(hits []string) Match {
	if len(hits) == 1 {
		return Match{Kind: Closest, Name: hits[0]}
	}
	sort.Strings(hits)
	m := Match{Kind: Ambiguous, Candidates: hits}
	if len(hits) > MaxCandidates {
		m.Candidates, m.Omitted = hits[:MaxCandidates], len(hits)-MaxCandidates
	}
	return m
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(s)
}

func filter(names []string, keep func(string) bool) []string {
	var out []string
	for _, n := range names {
		if keep(n) {
			out = append(out, n)
		}
	}
	return out
}

// Names compare token by token so an edit can fix a typo inside a word but
// never swap a word ("east" for "west") or a number ("-2" for "-1"): those
// name a sibling, not a misspelling.
func nearest(key string, names []string) []string {
	keyTokens := tokens(key)
	best := -1
	var hits []string
	for _, n := range names {
		d, ok := tokenDistance(keyTokens, tokens(normalize(n)))
		switch {
		case !ok:
		case best == -1 || d < best:
			best, hits = d, []string{n}
		case d == best:
			hits = append(hits, n)
		}
	}
	return hits
}

// tokenDistance sums per-token edits, allowing one edit per four runes of a
// token and none in a token holding a digit.
func tokenDistance(a, b []string) (int, bool) {
	if len(a) != len(b) {
		return 0, false
	}
	total := 0
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if hasDigit(a[i]) || hasDigit(b[i]) {
			return 0, false
		}
		runes := utf8.RuneCountInString(a[i])
		limit := runes / 4
		// The length gap bounds the distance from below; skipping on it keeps
		// a huge input from allocating a len(key)×len(name) matrix per name.
		if limit == 0 || abs(runes-utf8.RuneCountInString(b[i])) > limit {
			return 0, false
		}
		d := distance(a[i], b[i])
		if d > limit {
			return 0, false
		}
		total += d
	}
	return total, true
}

// containsWhole reports whether key occurs in name without cutting a number
// in two, so "web-1" does not match "web-12".
func containsWhole(name, key string) bool {
	first, _ := utf8.DecodeRuneInString(key)
	last, _ := utf8.DecodeLastRuneInString(key)
	for from := 0; ; {
		i := strings.Index(name[from:], key)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(key)
		before, _ := utf8.DecodeLastRuneInString(name[:start])
		after, _ := utf8.DecodeRuneInString(name[end:])
		if !(splitsNumber(before, first) || splitsNumber(last, after)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(name[start:])
		from = start + size
	}
}

func splitsNumber(a, b rune) bool {
	return unicode.IsDigit(a) && unicode.IsDigit(b)
}

func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '-' })
}

func compact(s string) string {
	return strings.Join(tokens(s), "")
}

func hasDigit(s string) bool {
	return strings.IndexFunc(s, unicode.IsDigit) >= 0
}

// Optimal string alignment, not plain Levenshtein: "stagign" is one edit from "staging".
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
