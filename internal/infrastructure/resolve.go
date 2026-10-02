package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetInfrastructureContextArgs are the input parameters for get_infrastructure_context.
type GetInfrastructureContextArgs struct {
	EntityType string                  `json:"entity_type" jsonschema:"(Required) Entity type: host, k8s_node, or k8s_pod. k8s_cluster is not valid input."`
	Selectors  InfrastructureSelectors `json:"selectors" jsonschema:"(Required) Identity labels for the entity."`
	Timestamp  int64                   `json:"timestamp,omitempty" jsonschema:"Unix seconds of the observation window. Defaults to now."`
	ClusterID  string                  `json:"cluster_id,omitempty" jsonschema:"Levitate datasource UUID. Defaults to the configured datasource."`
	Region     string                  `json:"region,omitempty" jsonschema:"AWS region sent as the region header. Defaults to the configured datasource region."`
}

// InfrastructureSelectors identify one host, node, or pod.
type InfrastructureSelectors struct {
	Cluster   string `json:"cluster,omitempty" jsonschema:"Kubernetes cluster name (k8s label). Required for k8s_node and k8s_pod."`
	Node      string `json:"node,omitempty" jsonschema:"Kubernetes node name. Required for k8s_node."`
	Namespace string `json:"namespace,omitempty" jsonschema:"Pod namespace. Required for k8s_pod when uid is omitted."`
	Pod       string `json:"pod,omitempty" jsonschema:"Pod name. Required for k8s_pod when uid is omitted."`
	UID       string `json:"uid,omitempty" jsonschema:"Pod UID. Accepted instead of namespace and pod."`
	HostID    string `json:"host_id,omitempty" jsonschema:"Host instance id. One of instance, host_id, or host_name is required for host."`
	HostName  string `json:"host_name,omitempty" jsonschema:"Host nodename / host_name."`
	Instance  string `json:"instance,omitempty" jsonschema:"Prometheus instance label (host:port)."`
	Job       string `json:"job,omitempty" jsonschema:"Optional Prometheus job label to disambiguate hosts."`
}

type resolvePayload struct {
	ClusterID  string            `json:"cluster_id"`
	EntityType string            `json:"entity_type"`
	Timestamp  int64             `json:"timestamp"`
	Selectors  map[string]string `json:"selectors"`
}

type resolveUI struct {
	Href string `json:"href"`
}

type resolveAnchor struct {
	UI resolveUI `json:"ui"`
}

type resolveResult struct {
	Anchor resolveAnchor `json:"anchor"`
}

// NewGetInfrastructureContextHandler returns the handler for get_infrastructure_context.
func NewGetInfrastructureContextHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetInfrastructureContextArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetInfrastructureContextArgs) (*mcp.CallToolResult, any, error) {
		payload, region, err := buildResolveRequest(cfg, args)
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		respBody, err := doJSONRequest(ctx, jsonCall{client: client, cfg: cfg, region: region, body: body})
		if err != nil {
			return nil, nil, err
		}
		return resultFromAPI(respBody), nil, nil
	}
}

func buildResolveRequest(cfg models.Config, args GetInfrastructureContextArgs) (resolvePayload, string, error) {
	entityType := strings.TrimSpace(args.EntityType)
	if err := validateEntityType(entityType); err != nil {
		return resolvePayload{}, "", err
	}
	clusterID := strings.TrimSpace(args.ClusterID)
	if clusterID == "" {
		clusterID = strings.TrimSpace(cfg.ClusterID)
	}
	if clusterID == "" {
		return resolvePayload{}, "", errors.New("cluster_id is required: pass cluster_id or configure LAST9_DATASOURCE")
	}
	region, err := resolveRegion(cfg, args.Region)
	if err != nil {
		return resolvePayload{}, "", err
	}
	ts := args.Timestamp
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	return resolvePayload{
		ClusterID:  clusterID,
		EntityType: entityType,
		Timestamp:  ts,
		Selectors:  selectorMap(args.Selectors),
	}, region, nil
}

func validateEntityType(entityType string) error {
	switch entityType {
	case "host", "k8s_node", "k8s_pod":
		return nil
	case "":
		return errors.New("entity_type is required")
	default:
		return fmt.Errorf("unsupported entity_type %q; use host, k8s_node, or k8s_pod", entityType)
	}
}

func resolveRegion(cfg models.Config, arg string) (string, error) {
	if strings.TrimSpace(arg) != "" {
		return strings.TrimSpace(arg), nil
	}
	if strings.TrimSpace(cfg.Region) != "" {
		return strings.TrimSpace(cfg.Region), nil
	}
	return "", errors.New("region is required: pass region or configure LAST9_DATASOURCE with a region")
}

func selectorMap(sel InfrastructureSelectors) map[string]string {
	out := map[string]string{}
	putSelector(out, "cluster", sel.Cluster)
	putSelector(out, "node", sel.Node)
	putSelector(out, "namespace", sel.Namespace)
	putSelector(out, "pod", sel.Pod)
	putSelector(out, "uid", sel.UID)
	putSelector(out, "host_id", sel.HostID)
	putSelector(out, "host_name", sel.HostName)
	putSelector(out, "instance", sel.Instance)
	putSelector(out, "job", sel.Job)
	return out
}

func putSelector(out map[string]string, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		out[key] = v
	}
}

func resultFromAPI(body []byte) *mcp.CallToolResult {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
	if href := parseAnchorHref(body); href != "" {
		result.Meta = deeplink.ToMeta(href)
	}
	return result
}

func parseAnchorHref(body []byte) string {
	var parsed resolveResult
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return parsed.Anchor.UI.Href
}
