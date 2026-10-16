package logs

import (
	"strings"
	"testing"
)

func TestRankLogPatterns_BreaksTiesByTemplateAndSplitsPastTopN(t *testing.T) {
	type item struct {
		template string
		count    int
	}
	byCountDesc := func(a, b item) int { return b.count - a.count }
	name := func(i item) string { return i.template }
	join := func(xs []item) string {
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, x.template)
		}
		return strings.Join(out, " ")
	}

	for _, tc := range []struct {
		topN              int
		wantTop, wantRest string
	}{
		{topN: 3, wantTop: "c d a", wantRest: "b"},
		{topN: 4, wantTop: "c d a b", wantRest: ""},
		{topN: 5, wantTop: "c d a b", wantRest: ""},
	} {
		// The tied "a" and "b" arrive out of template order, so the tie-break must reorder them.
		items := []item{{"b", 1}, {"c", 5}, {"a", 1}, {"d", 3}}
		top, rest := rankLogPatterns(items, tc.topN, name, byCountDesc)
		if got := join(top); got != tc.wantTop {
			t.Errorf("topN=%d: top = %q, want %q", tc.topN, got, tc.wantTop)
		}
		if got := join(rest); got != tc.wantRest {
			t.Errorf("topN=%d: rest = %q, want %q", tc.topN, got, tc.wantRest)
		}
	}
}
