package changelog

import (
	"fmt"
	"slices"
)

// CheckCitesPullRequest lists the [Unreleased] bullets that head adds over base and that
// do not cite pr. A changed bullet keeping the citation of one it replaced is a rewording,
// not an addition. Bullets citing nothing are left to Check.
func CheckCitesPullRequest(base, head string, pr int) []string {
	headBullets := UnreleasedBullets(head)
	inHead := map[string]bool{}
	for _, b := range headBullets {
		inHead[b] = true
	}
	inBase := map[string]bool{}
	replaced := map[string]int{}
	for _, b := range UnreleasedBullets(base) {
		inBase[b] = true
		if !inHead[b] {
			replaced[fmt.Sprint(CitedNumbers(b))]++
		}
	}

	var problems []string
	for _, b := range headBullets {
		numbers := CitedNumbers(b)
		if inBase[b] || numbers == nil || slices.Contains(numbers, pr) {
			continue
		}
		if key := fmt.Sprint(numbers); replaced[key] > 0 {
			replaced[key]--
			continue
		}
		problems = append(problems, fmt.Sprintf("%q is added by #%d; end it with (#%d)", b, pr, pr))
	}
	return problems
}
