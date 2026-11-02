package changelog

import (
	"slices"
	"strings"
	"testing"
)

const citationBase = `# Changelog

## [Unreleased]

### Added
- Pending entry (#12).
- Bumped ` + "`x/y`" + ` 1.2.3 → 1.2.4 (#20)

## [0.2.0] - 2026-10-28

### Added
- Shipped entry (#10).
`

func TestCheckCitesPullRequest(t *testing.T) {
	add := func(bullet string) string {
		return strings.Replace(citationBase, "- Pending entry (#12).", bullet+"\n- Pending entry (#12).", 1)
	}
	cases := []struct {
		name string
		head string
		want []string
	}{
		{name: "no new bullet", head: citationBase},
		{name: "new bullet cites this PR", head: add("- New tool (#30).")},
		{name: "new bullet cites the issue it closes", head: add("- New tool (#29)."),
			want: []string{`"New tool (#29)." is added by #30; end it with (#30)`}},
		{name: "new bullet cites an earlier PR in Unreleased", head: add("- New tool (#12)."),
			want: []string{`"New tool (#12)." is added by #30; end it with (#30)`}},
		{name: "collapsed bump adds this PR", head: strings.Replace(citationBase, "1.2.4 (#20)", "1.2.5 (#20, #30)", 1)},
		{name: "collapsed bump omits this PR", head: strings.Replace(citationBase, "1.2.4 (#20)", "1.2.5 (#20, #29)", 1),
			want: []string{"\"Bumped `x/y` 1.2.3 → 1.2.5 (#20, #29)\" is added by #30; end it with (#30)"}},
		{name: "reworded entry keeps its PR", head: strings.Replace(citationBase, "Pending entry (#12).", "Pending entry, reworded (#12).", 1)},
		{name: "rewording and a new bullet share the old PR",
			head: strings.Replace(add("- New tool (#12)."), "Pending entry (#12).", "Pending entry, reworded (#12).", 1),
			want: []string{`"Pending entry, reworded (#12)." is added by #30; end it with (#30)`}},
		{name: "uncited bullet is left to Check", head: add("- New tool.")},
		{name: "continuation line", head: add("- New tool that wraps\n  onto a second line (#31)."),
			want: []string{`"New tool that wraps onto a second line (#31)." is added by #30; end it with (#30)`}},
		{name: "release cut", head: strings.Replace(citationBase, "## [Unreleased]\n", "## [Unreleased]\n\n## [0.3.0] - 2026-11-01\n", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckCitesPullRequest(citationBase, tc.head, 30); !slices.Equal(got, tc.want) {
				t.Fatalf("CheckCitesPullRequest:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}
