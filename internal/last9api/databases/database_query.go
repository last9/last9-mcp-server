package databases

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"last9-mcp/internal/last9api"
)

const pathDatabaseQuery = "/database-query"

const opDatabaseQuery = "database query"

// Checked client-side so an oversized request fails with an actionable message instead of
// spending one of the organization's rate-limited calls on a guaranteed 400.
const maxWindow = 7 * 24 * time.Hour

// The bare "env" name reaches trace-discovered rows only, which would silently leave
// metrics-discovered rows unfiltered.
const envFilterName = "deployment_environment"

// Emits a regex matcher, preserving the semantics the tool has always had. An equality
// operator would turn "prod|staging" into a literal.
const envFilterOperator = "matches"

type Filter struct {
	Name     string `json:"name"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

func EnvFilter(env string) Filter {
	return Filter{Name: envFilterName, Operator: envFilterOperator, Value: env}
}

type TimeRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type Activity struct {
	Value float64 `json:"value"`
	Label string  `json:"label"`
	Unit  string  `json:"unit"`
}

type DatabaseEntity struct {
	ID             string            `json:"id"`
	DBSystem       string            `json:"db_system"`
	Host           string            `json:"host"`
	Env            string            `json:"env"`
	Sources        []string          `json:"sources"`
	Capabilities   []string          `json:"capabilities"`
	MetricsOnly    bool              `json:"metrics_only"`
	Activity       *Activity         `json:"activity"`
	ActivitySource string            `json:"activity_source"`
	Throughput     *float64          `json:"throughput"`
	P95LatencyMs   *float64          `json:"p95_latency_ms"`
	ErrorRate      *float64          `json:"error_rate"`
	ServiceCount   *int              `json:"service_count"`
	ResolvedLabels map[string]string `json:"resolved_labels"`
}

type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type DiscoverDatabasesResponse struct {
	Databases []DatabaseEntity `json:"databases"`
}

type DiscoverDatabasesInput struct {
	TimeRange TimeRange
	Filters   []Filter
}

type queryResult struct {
	Template string                     `json:"template"`
	Discover *DiscoverDatabasesResponse `json:"discover"`
	Partial  bool                       `json:"partial"`
	Errors   []FieldError               `json:"errors"`
}

type discoverRequest struct {
	Template  string    `json:"template"`
	ClusterID string    `json:"cluster_id"`
	TimeRange TimeRange `json:"time_range"`
	Filters   []Filter  `json:"filters,omitempty"`
}

// DiscoverDatabases calls POST /database-query with template=discover.
func DiscoverDatabases(
	ctx context.Context, c *last9api.Client, in DiscoverDatabasesInput,
) (*DiscoverDatabasesResponse, []FieldError, error) {
	if err := validateWindow(in.TimeRange); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", opDatabaseQuery, err)
	}

	body := discoverRequest{
		Template:  "discover",
		ClusterID: c.ClusterID(),
		TimeRange: in.TimeRange,
		Filters:   in.Filters,
	}

	var out queryResult
	err := c.Do(ctx, opDatabaseQuery,
		last9api.Request{Method: http.MethodPost, Path: pathDatabaseQuery, Body: body},
		&out,
		last9api.WithRegionHeader(),
	)
	if err != nil {
		return nil, nil, err
	}
	if out.Discover == nil {
		return &DiscoverDatabasesResponse{}, out.Errors, nil
	}
	return out.Discover, out.Errors, nil
}

func validateWindow(tr TimeRange) error {
	if tr.To <= tr.From {
		return errors.New("end time must be after start time")
	}
	span := time.Duration(tr.To-tr.From) * time.Second
	if span > maxWindow {
		return fmt.Errorf(
			"time range exceeds the 7 day maximum (got %s); request a shorter window",
			span.Round(time.Hour),
		)
	}
	return nil
}
