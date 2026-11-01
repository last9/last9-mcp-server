package nameresolution

import (
	"regexp"
	"strconv"
	"strings"
)

var metricPrefix = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*`)
var labelMatcher = regexp.MustCompile(`^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*("(?:\\.|[^"\\])*")\s*`)

type MetricSelector struct {
	Name       string
	original   string
	start, end int
	quoted     bool
	matchers   []string
}

func ParseMetricSelector(query string) (MetricSelector, bool) {
	p := MetricSelector{original: query}
	remaining := strings.TrimSpace(query)
	offset := strings.Index(query, remaining)
	if name := metricPrefix.FindString(remaining); name != "" {
		p.Name = name
		p.start = offset
		p.end = offset + len(name)
		remaining = strings.TrimSpace(remaining[len(name):])
	}
	if remaining == "" {
		return p, p.Name != ""
	}
	if !strings.HasPrefix(remaining, "{") || !strings.HasSuffix(remaining, "}") {
		return MetricSelector{}, false
	}
	offset = strings.Index(query, remaining) + 1
	body := remaining[1 : len(remaining)-1]
	seen := map[string]bool{}
	for strings.TrimSpace(body) != "" {
		positions := labelMatcher.FindStringSubmatchIndex(body)
		if positions == nil {
			return MetricSelector{}, false
		}
		key := body[positions[2]:positions[3]]
		op := body[positions[4]:positions[5]]
		value := body[positions[6]:positions[7]]
		decoded, err := strconv.Unquote(value)
		if err != nil || seen[key] {
			return MetricSelector{}, false
		}
		seen[key] = true
		if key == "__name__" {
			if p.Name != "" || op != "=" || decoded == "" {
				return MetricSelector{}, false
			}
			p.Name = decoded
			p.start = offset + positions[6]
			p.end = offset + positions[7]
			p.quoted = true
		} else {
			p.matchers = append(p.matchers, strings.TrimSpace(body[:positions[1]]))
		}
		offset += positions[1]
		body = body[positions[1]:]
		if body == "" {
			break
		}
		if body[0] != ',' {
			return MetricSelector{}, false
		}
		body = body[1:]
		offset++
	}
	return p, p.Name != ""
}

func (p MetricSelector) Scope() string {
	if len(p.matchers) == 0 {
		return `{__name__!=""}`
	}
	return "{" + strings.Join(p.matchers, ",") + "}"
}

func (p MetricSelector) WithName(name string) string {
	if p.quoted {
		name = strconv.Quote(name)
	} else if metricPrefix.FindString(name) != name {
		rest := p.original[p.end:]
		if brace := strings.Index(rest, "{"); brace >= 0 {
			separator := ""
			if len(p.matchers) > 0 {
				separator = ","
			}
			return p.original[:p.start] + rest[:brace+1] + "__name__=" + strconv.Quote(name) + separator + rest[brace+1:]
		}
		return p.original[:p.start] + "{__name__=" + strconv.Quote(name) + "}" + rest
	}
	return p.original[:p.start] + name + p.original[p.end:]
}
