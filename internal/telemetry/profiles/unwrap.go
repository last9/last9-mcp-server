package profiles

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func unwrapQueryRangeRows(body any) []map[string]any {
	switch typed := body.(type) {
	case []any:
		return asRecordRows(typed)
	case map[string]any:
		data, ok := typed["data"]
		if !ok || data == nil {
			return nil
		}
		switch payload := data.(type) {
		case []any:
			return asRecordRows(payload)
		case map[string]any:
			if resultType, _ := payload["resultType"].(string); resultType == "dataframe" {
				rawResult, _ := payload["result"].([]any)
				out := make([]map[string]any, 0, len(rawResult))
				for _, item := range rawResult {
					row, ok := item.(map[string]any)
					if !ok {
						continue
					}
					metric, ok := row["metric"].(map[string]any)
					if !ok || metric == nil {
						continue
					}
					out = append(out, metric)
				}
				return out
			}
		}
	}
	return nil
}

func asRecordRows(rows []any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok && m != nil {
			out = append(out, m)
		}
	}
	return out
}

func getServiceNameFromRow(row map[string]any) string {
	if name := strings.TrimSpace(asString(row["name"])); name != "" {
		return name
	}
	return strings.TrimSpace(asString(row["ServiceName"]))
}

func getRowNumber(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if n, ok := asFloat(row[key]); ok {
			return n
		}
	}
	return 0
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case string:
		if strings.TrimSpace(t) == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseProfileTimestamp(value any) string {
	if value == nil {
		return ""
	}
	switch t := value.(type) {
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return ""
		}
		if n, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parseProfileTimestamp(n)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
		if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
		return ""
	case float64:
		ms := t
		if t < 1e12 {
			ms = t * 1000
		}
		return time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339Nano)
	case int64:
		return parseProfileTimestamp(float64(t))
	case int:
		return parseProfileTimestamp(float64(t))
	default:
		return ""
	}
}

func mapFlamegraphRows(rows []map[string]any) []FlamegraphRow {
	out := make([]FlamegraphRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, FlamegraphRow{
			StackHash: asString(row["StackHash"]),
			Frames:    asString(row["Frames"]),
			Samples:   getRowNumber(row, "samples", "Value", "value"),
		})
	}
	return out
}

func buildProfileServiceIndex(sampleRows, lastProfileRows []map[string]any) []ProfileServiceIndexRow {
	lastByName := make(map[string]string)
	for _, row := range lastProfileRows {
		name := getServiceNameFromRow(row)
		if name == "" {
			continue
		}
		lastByName[name] = parseProfileTimestamp(firstNonNil(row["last_profile"], row["Timestamp"], row["timestamp"]))
	}

	type acc struct {
		samples       float64
		runtime       string
		runtimeSample float64
	}
	byName := make(map[string]*acc)
	for _, row := range sampleRows {
		name := getServiceNameFromRow(row)
		samples := getRowNumber(row, "samples", "Value", "value")
		if name == "" || samples <= 0 {
			continue
		}
		runtime := strings.TrimSpace(asString(firstNonNil(row["runtime"], row[ResourceRuntime])))
		existing, ok := byName[name]
		if !ok {
			byName[name] = &acc{samples: samples, runtime: runtime, runtimeSample: samples}
			continue
		}
		existing.samples += samples
		if runtime != "" && samples > existing.runtimeSample {
			existing.runtime = runtime
			existing.runtimeSample = samples
		}
	}

	var total float64
	for _, a := range byName {
		total += a.samples
	}

	out := make([]ProfileServiceIndexRow, 0, len(byName))
	for name, a := range byName {
		share := 0.0
		if total > 0 {
			share = a.samples / total
		}
		row := ProfileServiceIndexRow{
			Name:         name,
			Samples:      a.samples,
			Share:        share,
			SharePercent: share * 100,
			Runtime:      a.runtime,
		}
		if ts := lastByName[name]; ts != "" {
			row.LastProfileAt = ts
		}
		out = append(out, row)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Samples != out[j].Samples {
			return out[i].Samples > out[j].Samples
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}
