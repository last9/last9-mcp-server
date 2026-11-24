package suggest

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

	"last9-mcp/internal/models"
	"last9-mcp/internal/telemetry/logs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const logAttributeType = "log_attribute"

type attributeSuggestion struct {
	attr    logs.LogAttribute
	percent int
}

func checkLogAttributeScope(args DidYouMeanArgs) error {
	if strings.TrimSpace(args.Type) == logAttributeType {
		if args.LookbackMinutes < 0 {
			return fmt.Errorf("lookback_minutes must be at least 1, got %d", args.LookbackMinutes)
		}
		return nil
	}
	if strings.TrimSpace(args.Service) != "" || args.LookbackMinutes != 0 || args.StartTimeISO != "" || args.EndTimeISO != "" {
		return fmt.Errorf("service, lookback_minutes, start_time_iso and end_time_iso only apply to type %q", logAttributeType)
	}
	return nil
}

func suggestLogAttributes(ctx context.Context, client *http.Client, cfg models.Config, query string, args DidYouMeanArgs) (*mcp.CallToolResult, error) {
	attrs, err := logs.DiscoverLogAttributes(ctx, client, cfg, logs.LogAttributeScope{
		Service:         strings.TrimSpace(args.Service),
		LookbackMinutes: args.LookbackMinutes,
		StartTimeISO:    args.StartTimeISO,
		EndTimeISO:      args.EndTimeISO,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to discover log attributes: %w", err)
	}

	ranked := rankLogAttributes(query, attrs)
	if len(ranked) == 0 {
		return textResult(fmt.Sprintf("No log attributes resemble %q in this scope. List them with get_log_attributes_for_pipeline, or widen the window.", query)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Did you mean one of these for %q?\n\n", query)
	for _, s := range ranked[:min(len(ranked), maxDidYouMeanSuggestions)] {
		source := "indexed"
		if s.attr.Source == "body" {
			source = "body"
		}
		fmt.Fprintf(&sb, "  - %s (%d%% match, type: %s, filter_field: %s, source: %s)\n",
			s.attr.Name, s.percent, logAttributeType, s.attr.FilterField, source)
	}
	return textResult(sb.String()), nil
}

func rankLogAttributes(query string, attrs []logs.LogAttribute) []attributeSuggestion {
	var ranked []attributeSuggestion
	for _, a := range attrs {
		if p := NameSimilarity(query, a.Name); p > 0 {
			ranked = append(ranked, attributeSuggestion{attr: a, percent: p})
		}
	}
	slices.SortStableFunc(ranked, func(a, b attributeSuggestion) int {
		return cmp.Or(cmp.Compare(b.percent, a.percent), strings.Compare(a.attr.Name, b.attr.Name))
	})
	return ranked
}

// NameSimilarity is the percent did_you_mean ranks name by for query, ignoring
// word order, so a reordered name scores 100.
func NameSimilarity(query, name string) int {
	return int(math.Round(100 * wordSimilarity(words(query), words(name))))
}

// wordSimilarity credits each query word by its best match among the name's
// words, so "transaction id" finds "txn_id" (abbreviation) and "trackid"
// (embedded word), and divides by the longer word count.
func wordSimilarity(queryWords, nameWords []string) float64 {
	if len(queryWords) == 0 || len(nameWords) == 0 {
		return 0
	}
	total := 0.0
	for _, q := range queryWords {
		best := 0.0
		for _, n := range nameWords {
			best = max(best, wordCredit(q, n))
		}
		total += best
	}
	return total / float64(max(len(queryWords), len(nameWords)))
}

func wordCredit(a, b string) float64 {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	switch {
	case a == b:
		return 1
	case len(short) < 2:
		return 0
	case isAbbreviation(expandTrans(short), long):
		return 0.8
	case strings.Contains(long, short):
		return 0.5
	}
	return 0
}

// An x abbreviates "trans" (txn, xfer), which a letter subsequence cannot see.
func expandTrans(word string) string {
	if rest, ok := strings.CutPrefix(word, "tx"); ok {
		return "trans" + rest
	}
	if rest, ok := strings.CutPrefix(word, "x"); ok {
		return "trans" + rest
	}
	return word
}

func isAbbreviation(short, long string) bool {
	if short[0] != long[0] {
		return false
	}
	i := 0
	for j := 0; i < len(short) && j < len(long); j++ {
		if short[i] == long[j] {
			i++
		}
	}
	return i == len(short)
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
