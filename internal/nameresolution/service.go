package nameresolution

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

type ServiceLookup struct {
	Requested  string
	Telemetry  string
	Start, End int64
	Matchers   []string
	Index      string
}

func ResolveService(ctx context.Context, client *http.Client, cfg models.Config, in ServiceLookup) (Resolution, error) {
	if len(in.Requested) > 512 {
		return Resolution{}, fmt.Errorf("service name discovery: name exceeds 512 bytes")
	}
	matchers := append([]string(nil), in.Matchers...)
	var query string
	switch in.Telemetry {
	case "logs":
		matchers = append(matchers, `destination="logs"`)
		if in.Index != "" {
			index, ok := strings.CutPrefix(in.Index, "physical_index:")
			if !ok {
				return Resolution{}, fmt.Errorf("service name discovery is unavailable for this index")
			}
			matchers = append(matchers, `name="`+utils.EscapePromQLLabel(index)+`"`)
		}
		query = "sum by (service_name) (physical_index_service_count{" + strings.Join(matchers, ",") + "})"
	case "traces":
		if in.End <= in.Start {
			return Resolution{}, fmt.Errorf("service name discovery requires a non-empty window")
		}
		query = fmt.Sprintf("sum by (service_name) (last_over_time(trace_endpoint_count{%s}[%ds]))", strings.Join(matchers, ","), in.End-in.Start)
	default:
		return Resolution{}, fmt.Errorf("unsupported service discovery telemetry")
	}
	names, err := prom.DiscoverServiceNames(ctx, last9api.NewClient(client, cfg), prom.QueryInstantInput{
		Datasource: prom.Datasource{ReadURL: cfg.PrometheusReadURL, Username: cfg.PrometheusUsername, Password: cfg.PrometheusPassword},
		Query:      query, Timestamp: in.End,
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return Resolution{}, fmt.Errorf("service name discovery failed: %w", err)
	}
	return Resolve("service", in.Requested, Catalog{Names: names, Usable: true}), nil
}
