package changelog

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const placementBase = `# Changelog

## [Unreleased]

### Added
- Pending entry (#12).

## [0.2.0] - 2026-10-28

### Added
- Shipped entry (#10).
- Another shipped entry (#11).

## [0.1.0] - 2026-10-01

### Fixed
- Old entry without a number.
`

func TestCheckPlacement(t *testing.T) {
	addUnreleased := func(changelog, bullet string) string {
		return strings.Replace(changelog, "- Pending entry (#12).", bullet+"\n- Pending entry (#12).", 1)
	}
	addShipped := func(changelog, bullet string) string {
		return strings.Replace(changelog, "- Shipped entry (#10).", bullet+"\n- Shipped entry (#10).", 1)
	}
	cases := []struct {
		name string
		head string
		want []string
	}{
		{"unchanged", placementBase, nil},
		{"new entry under Unreleased", addUnreleased(placementBase, "- New entry (#20)."), nil},
		{
			"new entry merged under a released version",
			addShipped(placementBase, "- New entry (#20)."),
			[]string{`"New entry (#20)." is under [0.2.0], which did not ship #20; move it to [Unreleased]`},
		},
		{
			"pending entry moved under a released version",
			addShipped(strings.Replace(placementBase, "- Pending entry (#12).\n", "", 1), "- Pending entry (#12)."),
			[]string{`"Pending entry (#12)." is under [0.2.0], which did not ship #12; move it to [Unreleased]`},
		},
		{
			"new entry under both Unreleased and a released version",
			addShipped(addUnreleased(placementBase, "- New entry (#20)."), "- New entry (#20)."),
			[]string{
				`"New entry (#20)." is under [0.2.0], which did not ship #20; move it to [Unreleased]`,
				`"New entry (#20)." appears 2 times; a new entry appears once, under [Unreleased]`,
			},
		},
		{
			"new entry twice under Unreleased",
			addUnreleased(placementBase, "- New entry (#20).\n- New entry (#20)."),
			[]string{`"New entry (#20)." appears 2 times; a new entry appears once, under [Unreleased]`},
		},
		{
			"continuation line joins its bullet",
			addShipped(placementBase, "- New entry\n  wrapped (#20)."),
			[]string{`"New entry wrapped (#20)." is under [0.2.0], which did not ship #20; move it to [Unreleased]`},
		},
		{
			"released entry reworded",
			strings.Replace(placementBase, "- Shipped entry (#10).", "- Shipped entry, reworded (#10).", 1),
			nil,
		},
		{
			"released entry gains an external reference",
			strings.Replace(placementBase, "- Shipped entry (#10).", "- Shipped entry, as in nodejs/node#66871 (#10).", 1),
			nil,
		},
		{
			"released entry gains a number the release did not ship",
			strings.Replace(placementBase, "- Shipped entry (#10).", "- Shipped entry (#10, #20).", 1),
			[]string{`"Shipped entry (#10, #20)." is under [0.2.0], which did not ship #20; move it to [Unreleased]`},
		},
		{
			"new entry without a number under a released version",
			strings.Replace(placementBase, "- Old entry without a number.", "- Old entry without a number.\n- Another one.", 1),
			[]string{`"Another one." is new under [0.1.0]; move it to [Unreleased]`},
		},
		{
			"release cut",
			strings.Replace(placementBase, "## [Unreleased]\n\n### Added\n", "## [Unreleased]\n\n## [0.3.0] - 2026-11-02\n\n### Added\n", 1),
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckPlacement(placementBase, tc.head); !slices.Equal(got, tc.want) {
				t.Fatalf("CheckPlacement:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// CI's shallow checkout has no origin/main, so this skips there; the fixtures above pin
// the rules, and any checkout that has merged main enforces them on CHANGELOG.md.
func TestRepoChangelogEntriesStayUnreleased(t *testing.T) {
	mergeBase, err := exec.CommandContext(t.Context(), "git", "merge-base", "HEAD", "origin/main").Output()
	if err != nil {
		t.Skipf("no merge base with origin/main: %v", err)
	}
	base, err := exec.CommandContext(t.Context(), "git", "show", strings.TrimSpace(string(mergeBase))+":CHANGELOG.md").Output()
	if err != nil {
		t.Fatalf("read CHANGELOG.md at the merge base: %v", err)
	}
	head, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range CheckPlacement(string(base), string(head)) {
		t.Error(p)
	}
}
