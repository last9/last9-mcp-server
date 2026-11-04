package utils

import (
	"maps"
	"sort"
)

func trimStreamResponse(response map[string]any, items []any, env *ResultEnvelope) string {
	total := 0
	for _, item := range items {
		stream, ok := item.(map[string]any)
		if !ok {
			return trimQueryResponse(response, items, env)
		}
		values, ok := stream["values"].([]any)
		if !ok {
			return trimQueryResponse(response, items, env)
		}
		total += len(values)
	}
	data := response["data"].(map[string]any)
	probe, probeData := maps.Clone(response), maps.Clone(data)
	probe["data"] = probeData
	kept := sort.Search(total, func(n int) bool {
		probeData["result"] = streamLinePrefix(items, n+1)
		return len(formatIndentedJSON(probe)) > MaxResultTextBytes
	})
	data["result"] = streamLinePrefix(items, kept)
	env.Trim(total - kept)
	return formatIndentedJSON(response)
}

func streamLinePrefix(items []any, count int) []any {
	prefix := make([]any, 0, len(items))
	for _, item := range items {
		if count == 0 {
			break
		}
		stream := item.(map[string]any)
		values := stream["values"].([]any)
		if len(values) == 0 {
			continue
		}
		take := min(count, len(values))
		retained := maps.Clone(stream)
		retained["values"] = values[:take]
		prefix = append(prefix, retained)
		count -= take
	}
	return prefix
}
