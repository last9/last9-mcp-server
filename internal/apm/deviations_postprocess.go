// Package apm: post-processing of deviation results — deterministic sorting,
// the max_services cap, and the shared identity/priority helpers they rely on.
package apm

import (
	"math"
	"sort"
)

func sortDeviationResult(result *apmDeviationResult) {
	sort.Slice(result.Services, func(i, j int) bool {
		return identityLess(result.Services[i].ServiceName, result.Services[i].Env, result.Services[j].ServiceName, result.Services[j].Env)
	})
	sort.Slice(result.TelemetryChanges, func(i, j int) bool {
		return identityLess(result.TelemetryChanges[i].ServiceName, result.TelemetryChanges[i].Env, result.TelemetryChanges[j].ServiceName, result.TelemetryChanges[j].Env)
	})
	for _, board := range []*SignalLeaderboard{&result.Leaderboards.Reliability, &result.Leaderboards.Experience, &result.Leaderboards.SustainedLatency} {
		sortLeaderboard(board.Regressions)
		sortLeaderboard(board.Improvements)
	}
	sort.SliceStable(result.ThroughputShifts, func(i, j int) bool {
		left := math.Abs(result.ThroughputShifts[i].Comparison.AbsoluteDelta)
		right := math.Abs(result.ThroughputShifts[j].Comparison.AbsoluteDelta)
		if left != right {
			return left > right
		}
		return identityLess(result.ThroughputShifts[i].ServiceName, result.ThroughputShifts[i].Env, result.ThroughputShifts[j].ServiceName, result.ThroughputShifts[j].Env)
	})
}

func limitDeviationResult(result *apmDeviationResult, limit int) {
	if limit <= 0 {
		return
	}
	identities := make(map[string]struct{}, limit)
	add := func(serviceName, env string) {
		key := identityKey(serviceName, env)
		if _, ok := identities[key]; ok {
			return
		}
		if len(identities) >= limit {
			return
		}
		identities[key] = struct{}{}
	}
	// Deviating identities are kept in magnitude-priority order. The leaderboards
	// and ThroughputShifts are already magnitude-sorted by sortDeviationResult,
	// which runs before this cap, so capping here keeps the worst regression
	// visible and keeps the fleet follow-up aligned with the magnitude leader,
	// which leadingDeviationIdentity picks from the same ordered slices.
	for _, entries := range orderedDeviationSlices(*result) {
		for _, entry := range entries {
			add(entry.ServiceName, entry.Env)
		}
	}
	for _, change := range result.TelemetryChanges {
		add(change.ServiceName, change.Env)
	}
	// Fill any remaining capacity with the remaining (stable) services in the
	// alphabetical order result.Services is already sorted in. When no
	// identities deviate, this preserves the prior alphabetically-first slice.
	for _, service := range result.Services {
		add(service.ServiceName, service.Env)
	}
	result.Services = filterServices(result.Services, identities)
	result.TelemetryChanges = filterTelemetryChanges(result.TelemetryChanges, identities)
	result.ThroughputShifts = filterLeaderboardEntries(result.ThroughputShifts, identities)
	for _, board := range []*SignalLeaderboard{&result.Leaderboards.Reliability, &result.Leaderboards.Experience, &result.Leaderboards.SustainedLatency} {
		board.Regressions = filterLeaderboardEntries(board.Regressions, identities)
		board.Improvements = filterLeaderboardEntries(board.Improvements, identities)
	}
}

// orderedDeviationSlices returns the deviation slices in magnitude-priority
// order. This is the single source of that priority: limitDeviationResult uses
// it to decide which identities survive the max_services cap, and
// leadingDeviationIdentity uses it to pick the fleet follow-up target, so the
// two can never disagree about which identity leads.
func orderedDeviationSlices(result apmDeviationResult) [][]LeaderboardEntry {
	return [][]LeaderboardEntry{
		result.Leaderboards.Reliability.Regressions, result.Leaderboards.Reliability.Improvements,
		result.Leaderboards.Experience.Regressions, result.Leaderboards.Experience.Improvements,
		result.Leaderboards.SustainedLatency.Regressions, result.Leaderboards.SustainedLatency.Improvements,
		result.ThroughputShifts,
	}
}

// identityKey builds the composite map key for a service identity. NUL cannot
// appear in service or environment names, so the key cannot collide.
func identityKey(serviceName, env string) string {
	return serviceName + "\x00" + env
}

func filterServices(values []ServiceDeviation, identities map[string]struct{}) []ServiceDeviation {
	result := make([]ServiceDeviation, 0, len(values))
	for _, value := range values {
		if _, ok := identities[identityKey(value.ServiceName, value.Env)]; ok {
			result = append(result, value)
		}
	}
	return result
}

func filterTelemetryChanges(values []TelemetryChange, identities map[string]struct{}) []TelemetryChange {
	result := make([]TelemetryChange, 0, len(values))
	for _, value := range values {
		if _, ok := identities[identityKey(value.ServiceName, value.Env)]; ok {
			result = append(result, value)
		}
	}
	return result
}

func filterLeaderboardEntries(values []LeaderboardEntry, identities map[string]struct{}) []LeaderboardEntry {
	result := make([]LeaderboardEntry, 0, len(values))
	for _, value := range values {
		if _, ok := identities[identityKey(value.ServiceName, value.Env)]; ok {
			result = append(result, value)
		}
	}
	return result
}

func sortLeaderboard(entries []LeaderboardEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := comparisonMagnitude(entries[i].Comparison), comparisonMagnitude(entries[j].Comparison)
		if left != right {
			return left > right
		}
		return identityLess(entries[i].ServiceName, entries[i].Env, entries[j].ServiceName, entries[j].Env)
	})
}

func comparisonMagnitude(comparison SignalComparison) float64 {
	if comparison.RelativeDelta != nil {
		return math.Abs(*comparison.RelativeDelta)
	}
	return math.Abs(comparison.AbsoluteDelta)
}

func identityLess(leftService, leftEnv, rightService, rightEnv string) bool {
	return identityKey(leftService, leftEnv) < identityKey(rightService, rightEnv)
}
