// Package namematch resolves an approximate name typed by an agent to one of
// a known set of names, refusing to guess when more than one fits.
package namematch

import (
	"sort"
	"strings"
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
		func(key string) []string { return nearest(key, names) },
		func(key string) []string {
			if len([]rune(key)) < minFuzzyRunes {
				return nil
			}
			return filter(names, func(n string) bool { return strings.Contains(normalize(n), key) })
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
	if len(hits) > MaxCandidates {
		hits = hits[:MaxCandidates]
	}
	return Match{Kind: Ambiguous, Candidates: hits}
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

// One edit per four runes lets a typo resolve but not a different word.
func nearest(key string, names []string) []string {
	keyRunes := utf8.RuneCountInString(key)
	limit := keyRunes / 4
	if limit == 0 {
		return nil
	}
	best := limit + 1
	var hits []string
	for _, n := range names {
		norm := normalize(n)
		// The length gap bounds the distance from below; skipping on it keeps
		// a huge input from allocating a len(key)×len(name) matrix per name.
		if abs(keyRunes-utf8.RuneCountInString(norm)) > limit {
			continue
		}
		d := distance(key, norm)
		switch {
		case d > limit:
		case d < best:
			best, hits = d, []string{n}
		case d == best:
			hits = append(hits, n)
		}
	}
	return hits
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
