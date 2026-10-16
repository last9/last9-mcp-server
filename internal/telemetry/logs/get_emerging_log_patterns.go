package logs

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	changeNew         = "new"
	changeIncreased   = "increased"
	changeDecreased   = "decreased"
	changeDisappeared = "disappeared"

	comparisonRatePerMinute = "rate_per_minute"
	comparisonShareOfSample = "share_of_sample"

	defaultEmergingMinCount     = 3
	defaultEmergingChangeFactor = 2.0

	currentPatternWindow  = 0
	baselinePatternWindow = 1
)

var changeRank = map[string]int{changeNew: 0, changeIncreased: 1, changeDecreased: 2, changeDisappeared: 3}

// GetEmergingLogPatternsArgs is the input of the get_emerging_log_patterns tool.
// The embedded scope's time range is the current window.
type GetEmergingLogPatternsArgs struct {
	LogPatternScopeArgs
	BaselineStartTimeISO string  `json:"baseline_start_time_iso,omitempty" jsonschema:"Baseline window start, RFC3339. Set together with baseline_end_time_iso. Default: the window of equal length ending where the current window starts."`
	BaselineEndTimeISO   string  `json:"baseline_end_time_iso,omitempty" jsonschema:"Baseline window end, RFC3339, at or before the current window start. Set together with baseline_start_time_iso."`
	MinCount             int     `json:"min_count,omitempty" jsonschema:"Minimum lines a pattern needs in the window it grew in (or, for decreases, the baseline) to be reported (default: 3, or sample_size if smaller; maximum: sample_size)"`
	ChangeFactor         float64 `json:"change_factor,omitempty" jsonschema:"Frequency ratio between windows counted as significant, greater than 1 (default: 2, i.e. doubled or halved)"`
	TopN                 int     `json:"top_n,omitempty" jsonschema:"Changed patterns returned before the rest fold into other (default: 20, maximum: 100)"`
}

// GetEmergingLogPatternsResponse is the output of the get_emerging_log_patterns tool.
type GetEmergingLogPatternsResponse struct {
	Service           string                   `json:"service"`
	Env               string                   `json:"env,omitempty"`
	Current           LogPatternWindow         `json:"current"`
	Baseline          LogPatternWindow         `json:"baseline"`
	ComparisonBasis   string                   `json:"comparison_basis"`
	Patterns          []EmergingLogPattern     `json:"patterns"`
	UnchangedPatterns int                      `json:"unchanged_patterns"`
	Other             *EmergingLogPatternOther `json:"other,omitempty"`
	Note              string                   `json:"note,omitempty"`
}

// EmergingLogPattern is a template whose frequency changed between windows.
type EmergingLogPattern struct {
	Template string          `json:"template"`
	Change   string          `json:"change"`
	Ratio    *float64        `json:"ratio,omitempty"`
	Current  LogPatternStats `json:"current"`
	Baseline LogPatternStats `json:"baseline"`
	Sample   string          `json:"sample"`
}

// EmergingLogPatternOther totals the changed patterns that fell below top_n.
type EmergingLogPatternOther struct {
	Patterns      int `json:"patterns"`
	CurrentCount  int `json:"current_count"`
	BaselineCount int `json:"baseline_count"`
}

type emergingThresholds struct {
	minCount     int
	changeFactor float64
	topN         int
}

// NewGetEmergingLogPatternsHandler creates the handler for the get_emerging_log_patterns tool.
func NewGetEmergingLogPatternsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetEmergingLogPatternsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetEmergingLogPatternsArgs) (*mcp.CallToolResult, any, error) {
		scope, err := newLogPatternScope(args.LogPatternScopeArgs)
		if err != nil {
			return nil, nil, err
		}
		thresholds, err := args.thresholds(scope.sampleSize)
		if err != nil {
			return nil, nil, err
		}
		start, end, err := args.timeRange()
		if err != nil {
			return nil, nil, err
		}
		baselineStart, baselineEnd, err := args.baselineRange(start, end)
		if err != nil {
			return nil, nil, err
		}

		samples := scope.sampleWindows(ctx, client, cfg, [][2]time.Time{{start, end}, {baselineStart, baselineEnd}})
		for _, s := range samples {
			if s.err != nil {
				return nil, nil, s.err
			}
		}
		current, baseline := samples[currentPatternWindow].window, samples[baselinePatternWindow].window

		// Current lines go first so a pattern's sample comes from the window
		// being triaged whenever the pattern appears there.
		patterns, err := mineLogPatterns(samples[currentPatternWindow].lines, samples[baselinePatternWindow].lines)
		if err != nil {
			return nil, nil, err
		}

		comparison := newWindowComparison(current, baseline, end.Sub(start), baselineEnd.Sub(baselineStart))
		response := compareLogPatterns(patterns, comparison, thresholds)
		response.Service = scope.service
		response.Env = scope.env
		response.Current = current
		response.Baseline = baseline
		if baseline.SampledLines == 0 && current.SampledLines > 0 {
			response.Note = "Baseline window has no matching lines; every current pattern at or above min_count is reported as new."
		}

		text, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format response: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:    serviceLogsDeepLink(ctx, client, cfg, scope.search(start, end)),
			Content: []mcp.Content{&mcp.TextContent{Text: string(text)}},
		}, nil, nil
	}
}

type windowSample struct {
	lines  []LogEntry
	window LogPatternWindow
	err    error
}

// sampleWindows fetches the windows concurrently: triage prompts call this
// tool before anything else, so it should cost one round trip, not one per window.
func (s logPatternScope) sampleWindows(ctx context.Context, client *http.Client, cfg models.Config, ranges [][2]time.Time) []windowSample {
	samples := make([]windowSample, len(ranges))
	var wg sync.WaitGroup
	for i, r := range ranges {
		wg.Go(func() {
			samples[i].lines, samples[i].window, samples[i].err = s.sample(ctx, client, cfg, r[0], r[1])
		})
	}
	wg.Wait()
	return samples
}

// thresholds bounds min_count by the sample size: no pattern can count more
// lines than were sampled, so a larger min_count guarantees an empty answer.
func (args GetEmergingLogPatternsArgs) thresholds(sampleSize int) (emergingThresholds, error) {
	minCount, err := boundedPatternLimit("min_count", args.MinCount, min(defaultEmergingMinCount, sampleSize), sampleSize)
	if err != nil {
		return emergingThresholds{}, fmt.Errorf("%w (min_count cannot exceed sample_size)", err)
	}
	topN, err := boundedPatternLimit("top_n", args.TopN, defaultPatternTopN, maxPatternTopN)
	if err != nil {
		return emergingThresholds{}, err
	}
	factor := args.ChangeFactor
	if factor == 0 {
		factor = defaultEmergingChangeFactor
	}
	if !(factor > 1) || math.IsInf(factor, 0) {
		return emergingThresholds{}, fmt.Errorf("change_factor must be a finite number greater than 1, got %v", args.ChangeFactor)
	}
	return emergingThresholds{minCount: minCount, changeFactor: factor, topN: topN}, nil
}

func (args GetEmergingLogPatternsArgs) baselineRange(currentStart, currentEnd time.Time) (time.Time, time.Time, error) {
	startISO, endISO := strings.TrimSpace(args.BaselineStartTimeISO), strings.TrimSpace(args.BaselineEndTimeISO)
	if startISO == "" && endISO == "" {
		return currentStart.Add(-currentEnd.Sub(currentStart)), currentStart, nil
	}
	if startISO == "" || endISO == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("baseline_start_time_iso and baseline_end_time_iso must be set together")
	}
	start, err := utils.ParseToolTimestamp(startISO)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid baseline_start_time_iso: %w", err)
	}
	end, err := utils.ParseToolTimestamp(endISO)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid baseline_end_time_iso: %w", err)
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("baseline_start_time_iso must be before baseline_end_time_iso")
	}
	if end.After(currentStart) {
		return time.Time{}, time.Time{}, fmt.Errorf("baseline window must end at or before the current window start (%s)", currentStart.Format(time.RFC3339))
	}
	return start, end, nil
}

// windowComparison normalises counts so windows of different length or
// sampling compare fairly. Counts are exact per minute only while neither
// sample hit its cap; otherwise only each pattern's share of its sample is.
type windowComparison struct {
	basis                       string
	currentScale, baselineScale float64
}

func newWindowComparison(current, baseline LogPatternWindow, currentSpan, baselineSpan time.Duration) windowComparison {
	if current.SampleLimitReached || baseline.SampleLimitReached {
		return windowComparison{comparisonShareOfSample, float64(current.SampledLines), float64(baseline.SampledLines)}
	}
	return windowComparison{comparisonRatePerMinute, currentSpan.Minutes(), baselineSpan.Minutes()}
}

// ratio is the current frequency over the baseline frequency; both counts
// must be non-zero.
func (c windowComparison) ratio(current, baseline int) float64 {
	return (float64(current) * c.baselineScale) / (float64(baseline) * c.currentScale)
}

func compareLogPatterns(patterns []*logPattern, comparison windowComparison, thresholds emergingThresholds) GetEmergingLogPatternsResponse {
	response := GetEmergingLogPatternsResponse{ComparisonBasis: comparison.basis, Patterns: []EmergingLogPattern{}}
	for _, p := range patterns {
		current, baseline := p.stats(currentPatternWindow), p.stats(baselinePatternWindow)
		change, ratio := classifyPatternChange(current.Count, baseline.Count, comparison, thresholds)
		if change == "" {
			response.UnchangedPatterns++
			continue
		}
		response.Patterns = append(response.Patterns, EmergingLogPattern{
			Template: p.template(), Change: change, Ratio: ratio, Current: current, Baseline: baseline, Sample: p.sample,
		})
	}

	top, rest := rankLogPatterns(response.Patterns, thresholds.topN,
		func(p EmergingLogPattern) string { return p.Template },
		func(a, b EmergingLogPattern) int {
			return cmp.Or(
				cmp.Compare(changeRank[a.Change], changeRank[b.Change]),
				cmp.Compare(max(b.Current.Count, b.Baseline.Count), max(a.Current.Count, a.Baseline.Count)),
			)
		})
	response.Patterns = top
	if len(rest) > 0 {
		response.Other = &EmergingLogPatternOther{Patterns: len(rest)}
		for _, p := range rest {
			response.Other.CurrentCount += p.Current.Count
			response.Other.BaselineCount += p.Baseline.Count
		}
	}
	return response
}

func classifyPatternChange(current, baseline int, comparison windowComparison, thresholds emergingThresholds) (string, *float64) {
	switch {
	case baseline == 0 && current >= thresholds.minCount:
		return changeNew, nil
	case current == 0 && baseline >= thresholds.minCount:
		return changeDisappeared, nil
	case current == 0 || baseline == 0:
		return "", nil
	}
	ratio := comparison.ratio(current, baseline)
	rounded := math.Round(ratio*100) / 100
	switch {
	case ratio >= thresholds.changeFactor && current >= thresholds.minCount:
		return changeIncreased, &rounded
	case ratio*thresholds.changeFactor <= 1 && baseline >= thresholds.minCount:
		return changeDecreased, &rounded
	default:
		return "", nil
	}
}
