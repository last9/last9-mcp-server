package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// GoldenSignalsWindowsQuery scopes QueryGoldenSignalsWindows: Count
// consecutive windows of WindowMinutes each, the last one ending at End.
type GoldenSignalsWindowsQuery struct {
	Services      []string
	Env           string
	End           time.Time
	WindowMinutes int
	Count         int
}

// GoldenSignalsWindow is QueryGoldenSignals' answer for the window ending at End.
type GoldenSignalsWindow struct {
	End     time.Time
	Signals map[string]GoldenSignals
}

// QueryGoldenSignalsWindows returns the golden signals of consecutive equal
// windows, oldest first. Each signal is one range query stepped by the window
// length, so every point is that window's aggregate; the count does not
// multiply the queries. Windows the backend returns no point for are absent.
func QueryGoldenSignalsWindows(ctx context.Context, client *http.Client, cfg models.Config, q GoldenSignalsWindowsQuery) ([]GoldenSignalsWindow, error) {
	matcher := goldenSignalsMatcher(q.Services, q.Env)
	step := int64(q.WindowMinutes) * 60
	r := promRange{end: q.End.Unix(), window: step * int64(q.Count-1), step: step}

	rows := map[int64]map[string]*ServiceSummaryRow{}
	for _, class := range serviceSummaryQueried {
		r.op, r.query = "service summary "+class.key, class.query(matcher, q.WindowMinutes)
		byEnd, err := fetchPromRangeByEnd(ctx, client, cfg, r)
		if err != nil {
			return nil, err
		}
		for end, series := range byEnd {
			if rows[end] == nil {
				rows[end] = map[string]*ServiceSummaryRow{}
			}
			if err := mergeServiceSummarySeries(rows[end], series, class.set); err != nil {
				return nil, err
			}
		}
	}
	r.op, r.query = "golden signals latency", goldenLatencyQuery(matcher, q.WindowMinutes)
	latency, err := fetchPromRangeByEnd(ctx, client, cfg, r)
	if err != nil {
		return nil, err
	}

	byEnd := map[int64]map[string]GoldenSignals{}
	for end, counts := range rows {
		byEnd[end] = goldenSignalsFromCounts(counts, q.WindowMinutes)
	}
	for end, series := range latency {
		if byEnd[end] == nil {
			byEnd[end] = map[string]GoldenSignals{}
		}
		if err := addGoldenLatency(byEnd[end], series); err != nil {
			return nil, err
		}
	}
	windows := make([]GoldenSignalsWindow, 0, len(byEnd))
	for end, signals := range byEnd {
		if end <= r.end {
			windows = append(windows, GoldenSignalsWindow{End: time.Unix(end, 0).UTC(), Signals: signals})
		}
	}
	slices.SortFunc(windows, func(a, b GoldenSignalsWindow) int { return a.End.Compare(b.End) })
	return windows, nil
}

type promRange struct {
	op, query         string
	end, window, step int64
}

// fetchPromRangeByEnd runs a range query and regroups its points by evaluation
// time, each group shaped like an instant query's answer at that time.
func fetchPromRangeByEnd(ctx context.Context, client *http.Client, cfg models.Config, r promRange) (map[int64]apiPromInstantResp, error) {
	httpResp, err := utils.MakePromRangeAPIQuery(ctx, client, r.query, r.end-r.window, r.end, cfg, utils.PromResolution{Step: r.step})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, promErr(httpResp, r.op)
	}
	var parsed apiPromRangeResp
	if err := json.NewDecoder(httpResp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", r.op, err)
	}
	byEnd := map[int64]apiPromInstantResp{}
	for _, series := range parsed {
		for _, point := range series.Values {
			ts, ok := pointTime(point)
			if !ok {
				return nil, fmt.Errorf("failed to decode %s: a point has no timestamp", r.op)
			}
			byEnd[ts] = append(byEnd[ts], struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{series.Metric, point})
		}
	}
	return byEnd, nil
}

func pointTime(point []any) (int64, bool) {
	if len(point) < 1 {
		return 0, false
	}
	ts, ok := point[0].(float64)
	return int64(math.Round(ts)), ok
}
