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
	"us", "uk", "eu", "ap", "af", "sa", "ca", "me", "il", "mx", "cn", "in", "jp", "au",
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
	if hits, best, sure := nearest(key, names); len(hits) == 1 && best <= pickEdits && sure {
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

// nearest returns every name within the edit budget, the fewest edits among
// them, and whether every hit is safe to pick. An edit never swaps a word
// ("east" for "west") or a number ("-2" for "-1"): those name a sibling.
func nearest(key string, names []string) ([]string, int, bool) {
	keyTokens := tokens(key)
	best, sure := -1, true
	var hits []string
	for _, n := range names {
		if d, risk := tokenDistance(keyTokens, tokens(normalize(n))); risk != refused {
			hits = append(hits, n)
			sure = sure && risk == typo
			if best == -1 || d < best {
				best = d
			}
		}
	}
	return hits, best, sure
}

type editRisk int

const (
	typo editRisk = iota
	// doubtful edits may swap a region code not in siblingWords, so they are
	// offered, never picked; only insertions, deletions and transpositions
	// outside sibling words are picked.
	doubtful
	refused
)

// tokenDistance sums per-token edits, allowing one edit per four runes of a
// token, maxEdits in all, and none in a token holding a digit or swapping a
// sibling word.
func tokenDistance(a, b []string) (int, editRisk) {
	if len(a) != len(b) {
		return 0, refused
	}
	total, risk := 0, typo
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if hasDigit(a[i]) || hasDigit(b[i]) {
			return 0, refused
		}
		runes := utf8.RuneCountInString(a[i])
		limit := min(runes/4, maxEdits-total)
		// The length gap bounds the distance from below; skipping on it keeps
		// a huge input from allocating a len(key)×len(name) matrix per name.
		if limit == 0 || abs(runes-utf8.RuneCountInString(b[i])) > limit {
			return 0, refused
		}
		d := distance(a[i], b[i])
		if d > limit {
			return 0, refused
		}
		risk = max(risk, siblingRisk(a[i], b[i]))
		if risk == refused {
			return 0, refused
		}
		total += d
	}
	return total, risk
}

// siblingRisk refuses an edit touching a sibling word in both names ("canaryca"
// for "canarysa"); bar two-letter transpositions ("dmeo"), it doubts one touching
// it in one name or substituting letters, which may swap an unlisted "fr" for "tr".
func siblingRisk(a, b string) editRisk {
	ra, rb := []rune(a), []rune(b)
	pre := 0
	for pre < min(len(ra), len(rb)) && ra[pre] == rb[pre] {
		pre++
	}
	suf := 0
	for suf < min(len(ra), len(rb))-pre && ra[len(ra)-1-suf] == rb[len(rb)-1-suf] {
		suf++
	}
	spanA, spanB := ra[pre:len(ra)-suf], rb[pre:len(rb)-suf]
	inA, inB := touchesSiblingWord(ra, pre, len(ra)-suf), touchesSiblingWord(rb, pre, len(rb)-suf)
	switch {
	case inA && inB:
		return refused
	case transposes(spanA, spanB):
		return typo
	case inA || inB || len(spanA) == len(spanB):
		return doubtful
	}
	return typo
}

func transposes(a, b []rune) bool {
	return len(a) == 2 && len(b) == 2 && a[0] == b[1] && a[1] == b[0]
}

// touchesSiblingWord reports whether a sibling word in s overlaps s[from:to],
// or straddles from when the span is empty.
func touchesSiblingWord(s []rune, from, to int) bool {
	for _, w := range siblingWords {
		n := len(w)
		for i := 0; i+n <= len(s); i++ {
			if i < to && i+n > from && string(s[i:i+n]) == w {
				return true
			}
		}
	}
	return false
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
