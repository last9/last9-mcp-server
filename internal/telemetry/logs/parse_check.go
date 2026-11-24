package logs

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

type parseCheckScope struct {
	client         *http.Client
	cfg            models.Config
	startMs, endMs int64
	index          string
}

// parseCheckPlan accepts only filter* → parse+ → one $count aggregate: a filter
// after the parse drops rows on purpose, so only there must every filtered row
// reach the count.
func parseCheckPlan(stages []map[string]interface{}) (control []map[string]interface{}, countAlias, parser string, ok bool) {
	firstParse := 0
	for firstParse < len(stages) && stageType(stages[firstParse]) == "filter" {
		firstParse++
	}
	last := firstParse
	for last < len(stages) && stageType(stages[last]) == "parse" {
		last++
	}
	if last == firstParse || last != len(stages)-1 || stageType(stages[last]) != "aggregate" {
		return nil, "", "", false
	}
	aliases := slices.Sorted(maps.Keys(utils.ExtractCountAliases(stages[last:])))
	if len(aliases) == 0 {
		return nil, "", "", false
	}
	countAlias = aliases[0]

	control = append([]map[string]interface{}{}, stages[:firstParse]...)
	control = append(control, map[string]interface{}{
		"type": "aggregate",
		"aggregates": []interface{}{
			map[string]interface{}{"function": map[string]interface{}{"$count": []interface{}{}}, "as": countAlias},
		},
	})
	parser, _ = stages[firstParse]["parser"].(string)
	return control, countAlias, parser, true
}

func stageType(stage map[string]interface{}) string {
	t, _ := stage["type"].(string)
	return t
}

// The backend drops rows its parser cannot read without saying so, so the
// filtered rows are recounted without the parse to expose the gap.
func appendParseCheck(ctx context.Context, scope parseCheckScope, stages []map[string]interface{}, result map[string]interface{}) {
	control, countAlias, parser, ok := parseCheckPlan(stages)
	if !ok {
		return
	}
	if _, partial := result["l9_result"]; partial {
		return
	}
	afterParse, ok := countAggregateRows(result, countAlias)
	if !ok {
		return
	}

	controlResult, err := fetchParseCheckCount(ctx, scope, control)
	beforeParse, counted := 0.0, false
	if err == nil {
		beforeParse, counted = countAggregateRows(controlResult, countAlias)
	}
	if !counted {
		result["l9_parse_check"] = map[string]interface{}{
			"parser":           parser,
			"rows_after_parse": int64(math.Round(afterParse)),
			"error":            "could not count the rows matched before the parse stage; rows_after_parse may exclude rows the parser dropped",
		}
		return
	}
	if beforeParse <= afterParse {
		return
	}

	unparsed := int64(math.Round(beforeParse - afterParse))
	result["l9_parse_check"] = map[string]interface{}{
		"parser":            parser,
		"rows_before_parse": int64(math.Round(beforeParse)),
		"rows_after_parse":  int64(math.Round(afterParse)),
		"unparsed_rows":     unparsed,
		"note": fmt.Sprintf("%d rows matched the filters but are missing after the %q parse stage, which drops bodies it cannot parse (for example multi-line or header-prefixed key=value text). "+
			"Report the gap. To count those rows, match their bodies with a `regexp` parse using named groups and compare, or count them without the parse stage.", unparsed, parser),
	}
}

func countAggregateRows(result map[string]interface{}, countAlias string) (float64, bool) {
	_, items, err := extractResultItems(result)
	if err != nil {
		return 0, false
	}
	if len(items) == 0 {
		return 0, true
	}
	return utils.SumAggregateCount(result, map[string]struct{}{countAlias: {}})
}

// Bypasses fetchLogJSONQuery so the recount skips the count-sanity baseline
// the checked query already fetched.
func fetchParseCheckCount(ctx context.Context, scope parseCheckScope, pipeline []map[string]interface{}) (map[string]interface{}, error) {
	if scope.cfg.UseLogSearchAPI {
		return searchLogsByWindow(ctx, scope.client, scope.cfg, utils.LogSearchRequest{
			Op:       "logs parse check",
			Pipeline: pipeline,
			StartMs:  scope.startMs,
			EndMs:    scope.endMs,
			Index:    scope.index,
		})
	}
	return executeLogJSONQuery(ctx, scope.client, scope.cfg, pipeline, scope.startMs, scope.endMs, 0, scope.index)
}
