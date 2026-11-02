package changelog

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const baseChangelog = "# Changelog\n\n## [Unreleased]\n\n### Added\n- Existing entry (#100).\n\n" +
	"### Changed\n- Bumped `x/y` 1.2.3 → 1.2.4 (#90).\n\n## [0.1.0] - 2026-01-01\n\n### Fixed\n- Old entry (#50).\n"

func TestCheckAddedBulletsCite(t *testing.T) {
	addEntry := func(entry string) string {
		return strings.Replace(baseChangelog, "- Existing entry (#100).", entry+"\n- Existing entry (#100).", 1)
	}
	tests := []struct {
		name string
		base string // defaults to baseChangelog
		head string
		want []string // offending bullets, in order
	}{
		{name: "no changes", head: baseChangelog},
		{name: "new bullet cites this PR", head: addEntry("- New entry (#123).")},
		{name: "new bullet cites the issue instead", head: addEntry("- New entry (#122)."), want: []string{"New entry (#122)."}},
		{name: "new bullet cites nothing", head: addEntry("- New entry."), want: []string{"New entry."}},
		{name: "mid-sentence number is not a citation", head: addEntry("- Follow-up to (#123) work."), want: []string{"Follow-up to (#123) work."}},
		{name: "collapsed bump adds this PR", head: strings.Replace(baseChangelog, "1.2.4 (#90).", "1.2.5 (#90, #123).", 1)},
		{
			name: "collapsed bump without this PR",
			head: strings.Replace(baseChangelog, "1.2.4 (#90).", "1.2.5 (#90, #91).", 1),
			want: []string{"Bumped `x/y` 1.2.3 → 1.2.5 (#90, #91)."},
		},
		{
			name: "bullet moved within Unreleased is not new",
			head: strings.Replace(strings.Replace(baseChangelog, "- Existing entry (#100).\n", "", 1),
				"### Changed\n", "### Changed\n- Existing entry (#100).\n", 1),
		},
		{name: "duplicated bullet counts as new", head: addEntry("- Existing entry (#100)."), want: []string{"Existing entry (#100)."}},
		{name: "bullets outside Unreleased are ignored", head: strings.Replace(baseChangelog, "- Old entry (#50).", "- Old entry (#50).\n- Backfill (#7).", 1)},
		{name: "release cut empties Unreleased", head: strings.Replace(baseChangelog, "## [Unreleased]\n", "## [Unreleased]\n\n## [0.2.0] - 2026-02-01\n", 1)},
		{name: "first Unreleased section", base: "# Changelog\n", head: baseChangelog, want: []string{"Existing entry (#100).", "Bumped `x/y` 1.2.3 → 1.2.4 (#90)."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.base
			if base == "" {
				base = baseChangelog
			}
			problems := CheckAddedBulletsCite(base, tt.head, 123)
			if len(problems) != len(tt.want) {
				t.Fatalf("problems = %q, want one per %q", problems, tt.want)
			}
			for i, bullet := range tt.want {
				if !strings.Contains(problems[i], "(#123)") || !strings.Contains(problems[i], strconv.Quote(bullet)) {
					t.Errorf("problems[%d] = %q, want it to name (#123) and %q", i, problems[i], bullet)
				}
			}
		})
	}
}

// TestRepoChangelogCitesThisPullRequest checks the [Unreleased] bullets this PR
// adds against PR_NUMBER. CI runs it on pull requests with CHANGELOG_BASE_REF=HEAD^1.
func TestRepoChangelogCitesThisPullRequest(t *testing.T) {
	pr, baseRef := os.Getenv("PR_NUMBER"), os.Getenv("CHANGELOG_BASE_REF")
	if pr == "" || baseRef == "" {
		t.Skip("PR_NUMBER and CHANGELOG_BASE_REF not set")
	}
	number, err := strconv.Atoi(pr)
	if err != nil {
		t.Fatalf("PR_NUMBER=%q: %v", pr, err)
	}
	base, err := exec.Command("git", "show", baseRef+":CHANGELOG.md").Output()
	if err != nil {
		t.Fatalf("git show %s:CHANGELOG.md: %v", baseRef, err)
	}
	head, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range CheckAddedBulletsCite(string(base), string(head), number) {
		t.Error(p)
	}
}
