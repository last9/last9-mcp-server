package traces

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
)

const exceptionSeenMaxPoints = 60

const exceptionSeenPrecision = time.Millisecond

type exceptionSeen struct {
	First time.Time
	Last  time.Time
}

type exceptionSeenQuery struct {
	BaseFilter string
	Start, End time.Time
}

type exceptionSeenRangePlan struct {
	Config     models.Config
	BaseFilter string
	End        time.Time
	Window     time.Duration
	Step       time.Duration
}

// exceptionSeriesKey identifies a series by the labels get_exceptions groups by, raw, so range
// series match instant rows before any Unknown default is applied.
func exceptionSeriesKey(metric map[string]string) string {
	return strings.Join([]string{
		metric["exception_type"], metric["service_name"], metric["span_name"], metric["span_kind"], metric["env"],
	}, "\x00")
}

// exceptionSeenStep is the smallest whole-minute step that keeps a window within
// exceptionSeenMaxPoints points per series.
func exceptionSeenStep(window time.Duration) time.Duration {
	perPoint := window / exceptionSeenMaxPoints
	if window%exceptionSeenMaxPoints != 0 {
		perPoint++
	}
	step := perPoint.Truncate(time.Minute)
	if step < perPoint {
		step += time.Minute
	}
	return max(step, time.Minute)
}

// fetchExceptionSeen finds, per series, the first and last range step inside (Start, End] whose
// one-step bucket has occurrences.
func fetchExceptionSeen(ctx context.Context, client *http.Client, cfg models.Config, q exceptionSeenQuery) (map[string]exceptionSeen, error) {
	q = normalizeExceptionSeenQuery(q)
	if !q.End.After(q.Start) {
		return map[string]exceptionSeen{}, nil
	}
	step := exceptionSeenStep(q.End.Sub(q.Start))
	window := q.End.Sub(q.Start)
	fullWindow := window / step * step
	requests := make([]prom.QueryInput, 0, 2)
	if fullWindow > 0 {
		requests = append(requests, exceptionSeenRangeRequest(exceptionSeenRangePlan{
			Config:     cfg,
			BaseFilter: q.BaseFilter,
			End:        q.Start.Add(fullWindow),
			Window:     fullWindow,
			Step:       step,
		}))
	}
	if tail := window - fullWindow; tail > 0 {
		requests = append(requests, exceptionSeenRangeRequest(exceptionSeenRangePlan{
			Config:     cfg,
			BaseFilter: q.BaseFilter,
			End:        q.End,
			Step:       tail,
		}))
	}

	apiClient := last9api.NewClient(client, cfg)
	seenByKey := make(map[string]exceptionSeen)
	for _, request := range requests {
		series, err := prom.QuerySeries(ctx, apiClient, request)
		if err != nil {
			return nil, err
		}
		reduceExceptionSeen(seenByKey, series, q)
	}
	return seenByKey, nil
}

func normalizeExceptionSeenQuery(q exceptionSeenQuery) exceptionSeenQuery {
	start := q.Start.Truncate(exceptionSeenPrecision)
	if start.Before(q.Start) {
		start = start.Add(exceptionSeenPrecision)
	}
	q.Start = start
	q.End = q.End.Truncate(exceptionSeenPrecision)
	return q
}

func exceptionSeenRangeRequest(plan exceptionSeenRangePlan) prom.QueryInput {
	return prom.QueryInput{
		Datasource: prom.Datasource{
			ReadURL:  plan.Config.PrometheusReadURL,
			Username: plan.Config.PrometheusUsername,
			Password: plan.Config.PrometheusPassword,
		},
		Query:     buildExceptionsPromQL(plan.BaseFilter, prometheusDuration(plan.Step)),
		Timestamp: unixSeconds(plan.End),
		Window:    float64(plan.Window) / float64(time.Second),
		Step:      int64(plan.Step / time.Second),
	}
}

func unixSeconds(at time.Time) float64 {
	return float64(at.Unix()) + float64(at.Nanosecond())/float64(time.Second)
}

func timeFromUnixSeconds(seconds float64) time.Time {
	return time.UnixMilli(int64(math.Round(seconds * float64(time.Second/time.Millisecond)))).UTC()
}

func prometheusDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	if d%time.Second != 0 {
		return fmt.Sprintf("%dms", int64(d/time.Millisecond))
	}
	return fmt.Sprintf("%ds", int64(d/time.Second))
}

func reduceExceptionSeen(seenByKey map[string]exceptionSeen, series []prom.Series, q exceptionSeenQuery) {
	for _, s := range series {
		key := exceptionSeriesKey(s.Metric)
		seen := seenByKey[key]
		for _, point := range s.Values {
			if len(point) < 2 || !isOccurrenceCount(parsePromNumber(point[1])) {
				continue
			}
			at := timeFromUnixSeconds(parsePromNumber(point[0]))
			if !at.After(q.Start) || at.After(q.End) {
				continue
			}
			if seen.First.IsZero() || at.Before(seen.First) {
				seen.First = at
			}
			if at.After(seen.Last) {
				seen.Last = at
			}
		}
		if !seen.First.IsZero() {
			seenByKey[key] = seen
		}
	}
}

// isOccurrenceCount accepts only finite positive counts. Prometheus can serve "NaN" and "+Inf";
// NaN fails every comparison, so keep the check positive rather than skipping "<= 0".
func isOccurrenceCount(count float64) bool {
	return count > 0 && !math.IsInf(count, 1)
}
