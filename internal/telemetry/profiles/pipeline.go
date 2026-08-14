package profiles

import "strings"

type buildFilterOptions struct {
	includeProfileType bool
}

func normalizeProfileType(raw string) ProfileType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ProfileTypeAlloc):
		return ProfileTypeAlloc
	case string(ProfileTypeWall):
		return ProfileTypeWall
	case string(ProfileTypeCPU), "":
		return ProfileTypeCPU
	default:
		return ProfileTypeCPU
	}
}

func buildFilterConditions(filters ProfileFilters, opts buildFilterOptions) []any {
	conditions := make([]any, 0, 6)
	if opts.includeProfileType {
		profileType := filters.ProfileType
		if profileType == "" {
			profileType = DefaultProfileType
		}
		conditions = append(conditions, map[string]any{"$eq": []any{"ProfileType", string(profileType)}})
	}
	if filters.Service != "" {
		conditions = append(conditions, map[string]any{"$eq": []any{"ServiceName", filters.Service}})
	}
	if filters.Env != "" {
		conditions = append(conditions, map[string]any{"$eq": []any{ResourceEnv, filters.Env}})
	}
	if filters.Cluster != "" {
		conditions = append(conditions, map[string]any{"$eq": []any{ResourceCluster, filters.Cluster}})
	}
	if filters.Namespace != "" {
		conditions = append(conditions, map[string]any{"$eq": []any{ResourceNamespace, filters.Namespace}})
	}
	if filters.Runtime != "" {
		conditions = append(conditions, map[string]any{"$eq": []any{ResourceRuntime, filters.Runtime}})
	}
	return conditions
}

func buildFilterStage(filters ProfileFilters, opts buildFilterOptions) map[string]any {
	conditions := buildFilterConditions(filters, opts)
	if len(conditions) == 0 {
		return nil
	}
	if len(conditions) == 1 {
		return map[string]any{"type": "filter", "query": conditions[0]}
	}
	return map[string]any{
		"type":  "filter",
		"query": map[string]any{"$and": conditions},
	}
}

func withOptionalFilter(pipeline []any, filter map[string]any) []any {
	if filter == nil {
		return pipeline
	}
	return append([]any{filter}, pipeline...)
}

func flamegraphPipeline(filters ProfileFilters, limit int) []any {
	if limit <= 0 {
		limit = DefaultFlamegraphRowLimit
	}
	stages := []any{
		map[string]any{
			"type":     "aggregate",
			"function": map[string]any{"$sum": []any{"Value"}},
			"as":       "samples",
			"groupby":  map[string]any{"StackHash": "StackHash", "Body": "Frames"},
		},
		map[string]any{
			"type":   "select",
			"labels": nil,
			"order":  []any{"samples"},
			"limit":  limit,
		},
	}
	return withOptionalFilter(stages, buildFilterStage(filters, buildFilterOptions{includeProfileType: true}))
}

func serviceIndexSamplePipeline(filters ProfileFilters, limit int) []any {
	if limit <= 0 {
		limit = DefaultFlamegraphRowLimit
	}
	scope := ProfileFilters{
		Env:         filters.Env,
		Cluster:     filters.Cluster,
		Namespace:   filters.Namespace,
		Runtime:     filters.Runtime,
		ProfileType: filters.ProfileType,
	}
	stages := []any{
		map[string]any{
			"type":     "aggregate",
			"function": map[string]any{"$sum": []any{"Value"}},
			"as":       "samples",
			"groupby": map[string]any{
				"ServiceName":   "name",
				ResourceRuntime: "runtime",
			},
		},
		map[string]any{
			"type":   "select",
			"labels": nil,
			"order":  []any{"samples"},
			"limit":  limit,
		},
	}
	return withOptionalFilter(stages, buildFilterStage(scope, buildFilterOptions{includeProfileType: true}))
}

func serviceIndexLastPipeline(filters ProfileFilters, limit int) []any {
	if limit <= 0 {
		limit = DefaultFlamegraphRowLimit
	}
	scope := ProfileFilters{
		Env:         filters.Env,
		Cluster:     filters.Cluster,
		Namespace:   filters.Namespace,
		Runtime:     filters.Runtime,
		ProfileType: filters.ProfileType,
	}
	stages := []any{
		map[string]any{
			"type":     "aggregate",
			"function": map[string]any{"$max": []any{"Timestamp"}},
			"as":       "last_profile",
			"groupby":  map[string]any{"ServiceName": "name"},
		},
		map[string]any{
			"type":   "select",
			"labels": nil,
			"order":  nil,
			"limit":  limit,
		},
	}
	return withOptionalFilter(stages, buildFilterStage(scope, buildFilterOptions{includeProfileType: true}))
}
