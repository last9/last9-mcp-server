// Package paramhint enriches MCP schema-validation errors (-32602
// "unexpected additional properties") with the tool's valid parameter list
// and a single did-you-mean suggestion, so LLM clients can self-correct
// instead of burning calls on opaque rejections.
package paramhint

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Registry maps tool names to their valid top-level parameter names.
type Registry struct {
	params map[string][]string
}

func NewRegistry() *Registry {
	return &Registry{params: make(map[string][]string)}
}

// Register records the valid parameter names for a tool.
func (r *Registry) Register(toolName string, params []string) {
	sorted := append([]string(nil), params...)
	sort.Strings(sorted)
	r.params[toolName] = sorted
}

// ParamsOf derives the top-level parameter names from a tool's typed
// argument struct, using the same schema inference the SDK applies.
func ParamsOf[In any]() []string {
	schema, err := jsonschema.For[In](nil)
	if err != nil || schema == nil {
		return nil
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	return names
}

// quoted matches the Go %q-rendered key names inside the jsonschema-go
// validation error: unexpected additional properties ["match" "foo"].
var quoted = regexp.MustCompile(`"([^"]+)"`)

// offendingKeys extracts the rejected property names from the validation
// error text produced by jsonschema-go.
func offendingKeys(errText string) []string {
	idx := strings.Index(errText, "unexpected additional properties")
	if idx < 0 {
		return nil
	}
	tail := errText[idx:]
	if end := strings.Index(tail, "]"); end >= 0 {
		tail = tail[:end+1]
	}
	var keys []string
	for _, m := range quoted.FindAllStringSubmatch(tail, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// suggest returns the single closest valid parameter for an unknown key, or
// "" when no candidate is unambiguously close (distance must be small
// relative to key length). One suggestion only — repo convention.
func suggest(key string, valid []string) string {
	best := ""
	bestDist := -1
	for _, v := range valid {
		d := levenshtein(strings.ToLower(key), strings.ToLower(v))
		if bestDist == -1 || d < bestDist {
			best, bestDist = v, d
		}
	}
	if best == "" {
		return ""
	}
	limit := len(key) / 3
	if limit < 2 {
		limit = 2
	}
	if bestDist > limit {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// Hint builds the recovery hint for a failed tools/call, or "" when the
// error is not an unknown-parameter rejection or the tool is unregistered.
func (r *Registry) Hint(toolName, errText string) string {
	valid, ok := r.params[toolName]
	if !ok || len(valid) == 0 {
		return ""
	}
	keys := offendingKeys(errText)
	if len(keys) == 0 {
		return ""
	}
	var b strings.Builder
	for _, key := range keys {
		if s := suggest(key, valid); s != "" {
			fmt.Fprintf(&b, " Unknown parameter %q — did you mean %q?", key, s)
			break // one suggestion only
		}
	}
	fmt.Fprintf(&b, " Valid parameters for %s: %s.", toolName, strings.Join(valid, ", "))
	return b.String()
}

// Middleware intercepts failed tools/call requests and appends the recovery
// hint to schema-validation errors. All other traffic passes through
// untouched.
func Middleware(r *Registry) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err == nil || method != "tools/call" {
				return res, err
			}
			if !strings.Contains(err.Error(), "unexpected additional properties") {
				return res, err
			}
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return res, err
			}
			hint := r.Hint(params.Name, err.Error())
			if hint == "" {
				return res, err
			}
			return res, fmt.Errorf("%w%s", err, hint)
		}
	}
}
