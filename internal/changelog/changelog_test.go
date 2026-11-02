package changelog

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

const sample = `# Changelog

## [Unreleased]

### Added
- New tool (#12).
- Bumped ` + "`x/y`" + ` 1.2.3 → 1.2.5 (#231, #272)
- Cites the issue it closes (#481).

### Fixed
- No citation at all.
- Cites mid-sentence (#5) and then keeps going.

## [0.1.0] - 2026-01-01

### Added
- Released without a citation.
`

func TestUnreleasedBulletsStopAtTheFirstRelease(t *testing.T) {
	got := UnreleasedBullets(sample)
	want := []string{
		"New tool (#12).",
		"Bumped `x/y` 1.2.3 → 1.2.5 (#231, #272)",
		"Cites the issue it closes (#481).",
		"No citation at all.",
		"Cites mid-sentence (#5) and then keeps going.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnreleasedBullets:\n got %q\nwant %q", got, want)
	}
}

func TestUnreleasedBulletsWithoutUnreleasedSection(t *testing.T) {
	if got := UnreleasedBullets("# Changelog\n\n## [0.1.0]\n- old (#1).\n"); got != nil {
		t.Fatalf("got %q, want none", got)
	}
}

func TestCitedNumbers(t *testing.T) {
	cases := map[string][]int{
		"New tool (#12).":                 {12},
		"No trailing period (#12)":        {12},
		"Collapsed bump (#231, #272).":    {231, 272},
		"No citation at all.":             nil,
		"Mid-sentence (#5) then more.":    nil,
		"Bare number #12.":                nil,
		"Malformed list (#231,#272).":     nil,
		"Trailing space after it (#12). ": {12},
	}
	for bullet, want := range cases {
		if got := CitedNumbers(bullet); !reflect.DeepEqual(got, want) {
			t.Errorf("CitedNumbers(%q) = %v, want %v", bullet, got, want)
		}
	}
}

func TestCheckReportsBulletsWithoutCitation(t *testing.T) {
	problems, err := Check(context.Background(), sample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 ||
		!strings.Contains(problems[0], "No citation at all.") ||
		!strings.Contains(problems[1], "Cites mid-sentence (#5)") {
		t.Fatalf("problems = %q, want the two uncited Unreleased bullets", problems)
	}
}

func TestRepoChangelogCitesNumbers(t *testing.T) {
	text, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := Check(context.Background(), string(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
