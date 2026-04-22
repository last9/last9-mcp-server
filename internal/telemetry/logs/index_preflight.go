package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// physicalIndexResult holds the outcome of a preflight index lookup.
type physicalIndexResult struct {
	// Indexes is the list of unique physical index names the service writes to.
	// Empty means the service has no active log streams.
	Indexes []string
	// ServiceFound is false only when the metric returned zero series.
	// When the query itself fails we set ServiceFound=true to avoid false negatives.
	ServiceFound bool
}

// queryServiceLogIndex queries physical_index_service_count to discover
// which physical indexes a service is actively writing logs to.
// On any query failure it returns ServiceFound=true so callers fall back
// to normal behaviour rather than wrongly reporting no data.
func queryServiceLogIndex(ctx context.Context, client *http.Client, cfg models.Config, service, env string) physicalIndexResult {
	query := fmt.Sprintf(`physical_index_service_count{service_name=%q}`, service)
	if env != "" {
		query = fmt.Sprintf(`physical_index_service_count{service_name=%q,env=%q}`, service, env)
	}

	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, time.Now().UTC().Unix(), cfg)
	if err != nil {
		return physicalIndexResult{ServiceFound: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return physicalIndexResult{ServiceFound: true}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return physicalIndexResult{ServiceFound: true}
	}

	var results []struct {
		Metric map[string]string `json:"metric"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return physicalIndexResult{ServiceFound: true}
	}

	if len(results) == 0 {
		return physicalIndexResult{ServiceFound: false}
	}

	seen := make(map[string]struct{}, len(results))
	indexes := make([]string, 0, len(results))
	for _, r := range results {
		name := r.Metric["name"]
		if name == "" {
			name = "default"
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			indexes = append(indexes, name)
		}
	}

	return physicalIndexResult{Indexes: indexes, ServiceFound: true}
}

// resolveIndexFromPreflight returns the normalised index string to use for the
// log query (e.g. "physical_index:payments"), or "" to leave the index
// unset. It also returns a warning string when the service was found on
// multiple indexes.
func resolveIndexFromPreflight(result physicalIndexResult) (normalizedIndex string, multiIndexHint string) {
	if len(result.Indexes) == 1 {
		return "physical_index:" + result.Indexes[0], ""
	}
	if len(result.Indexes) > 1 {
		hint := fmt.Sprintf(
			"Service writes to multiple physical indexes %v — querying all. Specify index explicitly for faster results.",
			result.Indexes,
		)
		return "", hint
	}
	return "", ""
}
