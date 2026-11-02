package changelog

import (
	"fmt"
	"strings"
)

type entry struct{ version, bullet string }

type release struct {
	bullets map[string]bool
	cited   map[int]bool
}

func entries(changelog string) []entry {
	var out []entry
	version, inBullet := "", false
	for _, line := range strings.Split(changelog, "\n") {
		switch {
		case strings.HasPrefix(line, "## ["):
			version, _, _ = strings.Cut(strings.TrimPrefix(line, "## ["), "]")
			inBullet = false
		case version == "":
		case strings.HasPrefix(line, "- "):
			out = append(out, entry{version, strings.TrimSpace(line[2:])})
			inBullet = true
		case inBullet && strings.TrimSpace(line) != "" && strings.TrimLeft(line, " \t") != line:
			out[len(out)-1].bullet += " " + strings.TrimSpace(line)
		default:
			inBullet = false
		}
	}
	return out
}

// CheckPlacement lists head bullets that a released version did not ship, judged against
// base (the merge base), and new bullets that appear more than once. A version base lacks
// is a release cut and may hold any bullet.
func CheckPlacement(base, head string) []string {
	shipped := map[string]release{}
	known := map[string]bool{}
	for _, e := range entries(base) {
		r, ok := shipped[e.version]
		if !ok {
			r = release{bullets: map[string]bool{}, cited: map[int]bool{}}
			shipped[e.version] = r
		}
		r.bullets[e.bullet] = true
		for _, n := range CitedNumbers(e.bullet) {
			r.cited[n] = true
		}
		known[e.bullet] = true
	}

	var problems, added []string
	count := map[string]int{}
	for _, e := range entries(head) {
		if !known[e.bullet] {
			if count[e.bullet] == 0 {
				added = append(added, e.bullet)
			}
			count[e.bullet]++
		}
		r, released := shipped[e.version]
		if e.version == "Unreleased" || !released || r.bullets[e.bullet] {
			continue
		}
		numbers := CitedNumbers(e.bullet)
		if numbers == nil {
			problems = append(problems, fmt.Sprintf("%q is new under [%s]; move it to [Unreleased]", e.bullet, e.version))
			continue
		}
		var unshipped []string
		for _, n := range numbers {
			if !r.cited[n] {
				unshipped = append(unshipped, fmt.Sprintf("#%d", n))
			}
		}
		if len(unshipped) > 0 {
			problems = append(problems, fmt.Sprintf("%q is under [%s], which did not ship %s; move it to [Unreleased]",
				e.bullet, e.version, strings.Join(unshipped, ", ")))
		}
	}
	for _, bullet := range added {
		if count[bullet] > 1 {
			problems = append(problems, fmt.Sprintf("%q appears %d times; a new entry appears once, under [Unreleased]", bullet, count[bullet]))
		}
	}
	return problems
}
