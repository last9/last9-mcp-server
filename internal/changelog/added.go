package changelog

import (
	"fmt"
	"slices"
)

// AddedBullets returns head's [Unreleased] bullets that base's lacks, counting
// repeats, so a bullet edited in place (a collapsed dependency bump) is added.
func AddedBullets(base, head string) []string {
	existing := map[string]int{}
	for _, b := range UnreleasedBullets(base) {
		existing[b]++
	}
	var added []string
	for _, b := range UnreleasedBullets(head) {
		if existing[b] > 0 {
			existing[b]--
			continue
		}
		added = append(added, b)
	}
	return added
}

// CheckAddedBulletsCite lists the [Unreleased] bullets a pull request adds
// whose trailing citation does not include that pull request's number.
func CheckAddedBulletsCite(base, head string, pr int) []string {
	var problems []string
	for _, bullet := range AddedBullets(base, head) {
		if !slices.Contains(CitedNumbers(bullet), pr) {
			problems = append(problems, fmt.Sprintf("bullet added by this pull request must end with (#%d): %q", pr, bullet))
		}
	}
	return problems
}
