// Package namematch resolves an approximate name typed by an agent to one of
// a known set of names, refusing to guess when more than one fits.
package namematch

import (
	"slices"
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
	// Ambiguous means no name is safe to pick; Candidates lists the ones that
	// come close, possibly just one.
	Ambiguous
)

const MaxCandidates = 5

// minFuzzyRunes keeps short inputs ("st", "db") from matching unrelated names.
const minFuzzyRunes = 3

// A typo is picked only at pickEdits; up to maxEdits it is only offered as a
// candidate, however long the name.
const (
	pickEdits = 1
	maxEdits  = 2
)

// siblingWords name an environment or region; a name holding one is never
// a typo of a name that lacks it ("qa" for "uat", "af" for "ap").
var siblingWords = []string{
	"prod", "production", "preprod", "staging", "stage", "stg", "dev", "qa", "uat", "test", "sandbox", "canary",
	"east", "west", "north", "south", "central",
	"us", "eu", "ap", "af", "sa", "ca", "me", "il", "mx", "cn", "in", "jp", "au",
}

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
	if hits := filter(names, func(n string) bool { return normalize(n) == key }); len(hits) > 0 {
		return decide(hits)
	}
	if hits := filter(names, func(n string) bool { return sameCompact(normalize(n), key) }); len(hits) > 0 {
		return decide(hits)
	}
	if hits, best := nearest(key, names); len(hits) == 1 && best <= pickEdits {
		return decide(hits)
	} else if len(hits) > 0 {
		return candidates(hits)
	}
	// A unique substring may still name a sibling ("ingest-eu" for
	// "ingest-eu-2"), so it is offered, never picked.
	if len([]rune(key)) >= minFuzzyRunes {
		if hits := filter(names, func(n string) bool { return containsWhole(normalize(n), key) }); len(hits) > 0 {
			return candidates(hits)
		}
	}
	return Match{}
}

func decide(hits []string) Match {
	if len(hits) == 1 {
		return Match{Kind: Closest, Name: hits[0]}
	}
	return candidates(hits)
}

func candidates(hits []string) Match {
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

// nearest returns every name within the edit budget and the fewest edits
// among them. An edit never swaps a word ("east" for "west") or a number
// ("-2" for "-1"): those name a sibling, not a misspelling.
func nearest(key string, names []string) ([]string, int) {
	keyTokens := tokens(key)
	best := -1
	var hits []string
	for _, n := range names {
		if d, ok := tokenDistance(keyTokens, tokens(normalize(n))); ok {
			hits = append(hits, n)
			if best == -1 || d < best {
				best = d
			}
		}
	}
	return hits, best
}

// tokenDistance sums per-token edits, allowing one edit per four runes of a
// token, maxEdits in all, and none in a token holding a digit or more
// occurrences of a sibling word than the other.
func tokenDistance(a, b []string) (int, bool) {
	if len(a) != len(b) {
		return 0, false
	}
	total := 0
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if hasDigit(a[i]) || hasDigit(b[i]) || addsSiblingWord(a[i], b[i]) {
			return 0, false
		}
		runes := utf8.RuneCountInString(a[i])
		limit := min(runes/4, maxEdits-total)
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

// Counting, not presence: "canaryca" holds "ca" twice, so the "ca" in canary
// does not let it stand for "canarysa".
func addsSiblingWord(typed, name string) bool {
	return slices.ContainsFunc(siblingWords, func(w string) bool {
		return strings.Count(typed, w) > strings.Count(name, w)
	})
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

// sameCompact ignores separators but not where they split numbers, so
// "db-1-2-3" stays apart from "db123" while "web1" matches "web-1".
func sameCompact(a, b string) bool {
	return compact(a) == compact(b) && slices.Equal(numbers(a), numbers(b))
}

func compact(s string) string {
	return strings.Join(tokens(s), "")
}

func numbers(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
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
