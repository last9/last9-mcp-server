// Package changelog checks that every CHANGELOG.md [Unreleased] bullet ends by
// citing the pull request that made the change, as AGENTS.md requires.
package changelog

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var citation = regexp.MustCompile(`\((#\d+(?:, #\d+)*)\)\.?$`)

func UnreleasedBullets(changelog string) []string {
	var bullets []string
	inUnreleased, inBullet := false, false
	for _, line := range strings.Split(changelog, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			if inUnreleased {
				return bullets
			}
			inUnreleased = strings.HasPrefix(line, "## [Unreleased]")
			inBullet = false
		case !inUnreleased:
		case strings.HasPrefix(line, "- "):
			bullets = append(bullets, strings.TrimSpace(line[2:]))
			inBullet = true
		case inBullet && strings.TrimSpace(line) != "" && strings.TrimLeft(line, " \t") != line:
			bullets[len(bullets)-1] += " " + strings.TrimSpace(line)
		default:
			inBullet = false
		}
	}
	return bullets
}

func CitedNumbers(bullet string) []int {
	m := citation.FindStringSubmatch(strings.TrimSpace(bullet))
	if m == nil {
		return nil
	}
	var numbers []int
	for _, ref := range strings.Split(m[1], ", ") {
		n, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))
		if err != nil {
			return nil
		}
		numbers = append(numbers, n)
	}
	return numbers
}

type IsPullRequest func(ctx context.Context, number int) (bool, error)

func Check(ctx context.Context, changelog string, isPR IsPullRequest) ([]string, error) {
	var problems []string
	var cited []int
	seen := map[int]bool{}
	for _, bullet := range UnreleasedBullets(changelog) {
		numbers := CitedNumbers(bullet)
		if numbers == nil {
			problems = append(problems, fmt.Sprintf("bullet does not end with its pull request number, e.g. (#123): %q", bullet))
		}
		for _, n := range numbers {
			if !seen[n] {
				seen[n] = true
				cited = append(cited, n)
			}
		}
	}
	if isPR == nil {
		return problems, nil
	}
	for _, n := range cited {
		ok, err := isPR(ctx, n)
		if err != nil {
			return nil, fmt.Errorf("resolve #%d: %w", n, err)
		}
		if !ok {
			problems = append(problems, fmt.Sprintf("#%d is not a pull request: cite the PR that made the change, not the issue it closes", n))
		}
	}
	return problems, nil
}
