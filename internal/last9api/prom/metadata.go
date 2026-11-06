package prom

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/utils"
)

const pathMetadata = "/prom_metadata"

var (
	ErrMetadataUnsupported = errors.New("datasource metadata is unsupported")
	metricNamePattern      = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
)

type GetMetadataInput struct {
	Datasource
	Metric string `json:"metric"`
}

type MetadataDefinition struct {
	Type string  `json:"type"`
	Unit *string `json:"unit"`
	Help *string `json:"help"`
}

type GetMetadataResponse struct {
	Status string                          `json:"status"`
	Data   map[string][]MetadataDefinition `json:"data"`
}

func ValidMetricName(name string) bool { return metricNamePattern.MatchString(name) }

// GetMetadata calls POST /prom_metadata.
func GetMetadata(ctx context.Context, c *last9api.Client, in GetMetadataInput) (GetMetadataResponse, error) {
	var out GetMetadataResponse
	if !ValidMetricName(in.Metric) {
		return out, fmt.Errorf("invalid metric name %q", in.Metric)
	}
	err := c.Do(ctx, "metric metadata", last9api.Request{Method: http.MethodPost, Path: pathMetadata, Body: in}, &out,
		last9api.WithRequiredBody(), last9api.WithMaxResponseBytes(256<<10))
	if err != nil {
		var upstream *utils.UpstreamHTTPError
		if errors.As(err, &upstream) && upstream.StatusCode == http.StatusNotImplemented {
			return out, ErrMetadataUnsupported
		}
		return out, err
	}
	if out.Status != "success" || out.Data == nil {
		return out, errors.New("metric metadata: invalid success envelope")
	}
	for name, definitions := range out.Data {
		if name != in.Metric || definitions == nil {
			return out, errors.New("metric metadata: invalid metric definitions")
		}
		for _, d := range definitions {
			if strings.TrimSpace(d.Type) == "" || d.Unit == nil || d.Help == nil {
				return out, errors.New("metric metadata: incomplete definition")
			}
		}
	}
	return out, nil
}
