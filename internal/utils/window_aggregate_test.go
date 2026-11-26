package utils

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeWindowAggregateDays(t *testing.T) {
	for _, tc := range []struct {
		window, want []interface{}
	}{
		{window: []interface{}{"1", "days"}, want: []interface{}{"24", "hours"}},
		{window: []interface{}{"7", "days"}, want: []interface{}{"168", "hours"}},
		{window: []interface{}{"2", "Day"}, want: []interface{}{"48", "hours"}},
		{window: []interface{}{float64(3), "days"}, want: []interface{}{"72", "hours"}},
		{window: []interface{}{"106751", "days"}, want: []interface{}{"2562024", "hours"}},
		{window: []interface{}{"5", "minutes"}, want: []interface{}{"5", "minutes"}},
		{window: []interface{}{"24", "hours"}, want: []interface{}{"24", "hours"}},
	} {
		pipeline := []map[string]interface{}{{"type": "window_aggregate", "window": tc.window}}
		if err := NormalizeWindowAggregateDays(pipeline); err != nil {
			t.Fatalf("%v: %v", tc.window, err)
		}
		if got := pipeline[0]["window"]; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.window, got, tc.want)
		}
	}
}

func TestNormalizeWindowAggregateDaysRejectsInvalidDayCounts(t *testing.T) {
	for _, amount := range []interface{}{
		"106752", "384307168202282325", "9223372036854775808", "0", "-1", "1.5", " 1", "", "one",
		float64(0), float64(1.5), float64(106752), float64(1e19), nil, true,
	} {
		pipeline := []map[string]interface{}{{"type": "window_aggregate", "window": []interface{}{amount, "days"}}}
		if err := NormalizeWindowAggregateDays(pipeline); err == nil {
			t.Errorf("%#v days: want error, window became %v", amount, pipeline[0]["window"])
		}
	}
}

func TestNormalizeWindowAggregateDaysErrorQuotesTheWindow(t *testing.T) {
	pipeline := []map[string]interface{}{{"type": "window_aggregate", "window": []interface{}{" 7 ", "days"}}}
	err := NormalizeWindowAggregateDays(pipeline)
	if err == nil || !strings.Contains(err.Error(), `window [" 7 ","days"]`) {
		t.Errorf("got %v, want the window quoted as JSON", err)
	}
}

func TestNormalizeWindowAggregateDaysOnlyTouchesWindowAggregate(t *testing.T) {
	pipeline := []map[string]interface{}{{"type": "aggregate", "window": []interface{}{"1", "days"}}}
	if err := NormalizeWindowAggregateDays(pipeline); err != nil {
		t.Fatal(err)
	}
	if got := pipeline[0]["window"]; !reflect.DeepEqual(got, []interface{}{"1", "days"}) {
		t.Errorf("aggregate stage window changed to %v", got)
	}
}
