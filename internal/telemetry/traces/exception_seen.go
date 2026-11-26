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

type exceptionSeen struct {
	First time.Time
	Last  time.Time
}

type exceptionSeenQuery struct {
	BaseFilter string
	Start, End time.Time
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
	perPoint := (window + exceptionSeenMaxPoints - 1) / exceptionSeenMaxPoints
	step := perPoint.Truncate(time.Minute)
	if step < perPoint {
		step += time.Minute
	}
	return max(step, time.Minute)
}

// fetchExceptionSeen finds, per series, the first and last range step inside (Start, End] whose
// one-step bucket has occurrences.
func fetchExceptionSeen(ctx context.Context, client *http.Client, cfg models.Config, q exceptionSeenQuery) (map[string]exceptionSeen, error) {
	step := exceptionSeenStep(q.End.Sub(q.Start))
	stepSeconds := int64(step / time.Second)
	query := buildExceptionsPromQL(q.BaseFilter, fmt.Sprintf("%dm", int64(step/time.Minute)))

	series, err := prom.QuerySeries(ctx, last9api.NewClient(client, cfg), prom.QueryInput{
		Datasource: prom.Datasource{
			ReadURL:  cfg.PrometheusReadURL,
			Username: cfg.PrometheusUsername,
			Password: cfg.PrometheusPassword,
		},
		Query:     query,
		Timestamp: q.End.Unix(),
		Window:    q.End.Unix() - q.Start.Unix(),
		Step:      stepSeconds,
	})
	if err != nil {
		return nil, err
	}

	seenByKey := make(map[string]exceptionSeen, len(series))
	for _, s := range series {
		var seen exceptionSeen
		for _, point := range s.Values {
			if len(point) < 2 || !isOccurrenceCount(parsePromNumber(point[1])) {
				continue
			}
			at := time.Unix(int64(parsePromNumber(point[0])), 0).UTC()
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
			seenByKey[exceptionSeriesKey(s.Metric)] = seen
		}
	}
	return seenByKey, nil
}

// isOccurrenceCount accepts only finite positive counts. Prometheus can serve "NaN" and "+Inf";
// NaN fails every comparison, so keep the check positive rather than skipping "<= 0".
func isOccurrenceCount(count float64) bool {
	return count > 0 && !math.IsInf(count, 1)
}
