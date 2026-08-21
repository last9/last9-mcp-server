package infrastructure

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func entitiesFromMetrics(entityType, clusterID, org string, metrics []map[string]string) []searchEntity {
	seen := map[string]struct{}{}
	out := make([]searchEntity, 0, len(metrics))
	for _, metric := range metrics {
		ent, ok := entityFromMetric(entityType, clusterID, org, metric)
		if !ok {
			continue
		}
		if _, dup := seen[ent.ID]; dup {
			continue
		}
		seen[ent.ID] = struct{}{}
		out = append(out, ent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func entityFromMetric(entityType, clusterID, org string, metric map[string]string) (searchEntity, bool) {
	switch entityType {
	case "host":
		return hostSearchEntity(clusterID, org, metric)
	case "k8s_cluster":
		return clusterSearchEntity(clusterID, org, metric)
	case "k8s_node":
		return nodeSearchEntity(clusterID, org, metric)
	default:
		return podSearchEntity(clusterID, org, metric)
	}
}

func hostSearchEntity(clusterID, org string, metric map[string]string) (searchEntity, bool) {
	hostID := firstNonEmpty(metric["instance_id"], metric["nodename"], metric["instance_name"], metric["instance"], metric["host_name"])
	if hostID == "" {
		return searchEntity{}, false
	}
	attrs := omitEmptyAttrs(map[string]string{
		"host_id":   hostID,
		"host_name": firstNonEmpty(metric["host_name"], metric["nodename"], metric["instance_name"], hostID),
		"instance":  metric["instance"],
		"job":       metric["job"],
	})
	return searchEntity{
		ID:         strings.Join([]string{"host", clusterID, hostID}, ":"),
		Type:       "host",
		Attributes: attrs,
		UI:         searchUI{Href: hostSearchHref(org, clusterID, attrs)},
	}, true
}

func nodeSearchEntity(clusterID, org string, metric map[string]string) (searchEntity, bool) {
	node := strings.TrimSpace(metric["node"])
	if node == "" {
		return searchEntity{}, false
	}
	cluster := strings.TrimSpace(metric["cluster"])
	attrs := omitEmptyAttrs(map[string]string{"cluster": cluster, "node": node})
	return searchEntity{
		ID:         strings.Join([]string{"k8s-node", clusterID, cluster, node}, ":"),
		Type:       "k8s_node",
		Attributes: attrs,
		UI:         searchUI{Href: nodeSearchHref(org, clusterID, cluster, node)},
	}, true
}

func clusterSearchEntity(clusterID, org string, metric map[string]string) (searchEntity, bool) {
	cluster := strings.TrimSpace(metric["cluster"])
	if cluster == "" {
		return searchEntity{}, false
	}
	attrs := map[string]string{"cluster": cluster}
	return searchEntity{
		ID:         strings.Join([]string{"k8s-cluster", clusterID, cluster}, ":"),
		Type:       "k8s_cluster",
		Attributes: attrs,
		UI:         searchUI{Href: clusterSearchHref(org, clusterID, cluster)},
	}, true
}

func podSearchEntity(clusterID, org string, metric map[string]string) (searchEntity, bool) {
	pod := strings.TrimSpace(metric["pod"])
	namespace := strings.TrimSpace(metric["namespace"])
	uid := strings.TrimSpace(metric["uid"])
	if pod == "" && uid == "" {
		return searchEntity{}, false
	}
	cluster := strings.TrimSpace(metric["cluster"])
	idTail := uid
	if idTail == "" {
		idTail = pod
	}
	attrs := omitEmptyAttrs(map[string]string{
		"cluster":   cluster,
		"namespace": namespace,
		"pod":       pod,
		"uid":       uid,
		"node":      metric["node"],
	})
	return searchEntity{
		ID:         strings.Join([]string{"k8s-pod", clusterID, cluster, namespace, idTail}, ":"),
		Type:       "k8s_pod",
		Attributes: attrs,
		UI:         searchUI{Href: podSearchHref(org, clusterID, attrs)},
	}, true
}

func filterEntities(entities []searchEntity, query string) []searchEntity {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entities
	}
	out := make([]searchEntity, 0, len(entities))
	for _, ent := range entities {
		if entityMatchesQuery(ent, q) {
			out = append(out, ent)
		}
	}
	return out
}

func entityMatchesQuery(ent searchEntity, query string) bool {
	if strings.Contains(strings.ToLower(ent.ID), query) {
		return true
	}
	for _, value := range ent.Attributes {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func paginateEntities(entityType string, entities []searchEntity, limit int, cursor string, truncated bool) (searchPage, error) {
	offset, err := parseSearchCursor(cursor)
	if err != nil {
		return searchPage{}, err
	}
	if offset > len(entities) {
		offset = len(entities)
	}
	end := offset + limit
	if end > len(entities) {
		end = len(entities)
	}
	page := searchPage{EntityType: entityType, Entities: entities[offset:end], Truncated: truncated}
	if page.Entities == nil {
		page.Entities = []searchEntity{}
	}
	if end < len(entities) {
		page.NextCursor = strconv.Itoa(end)
	}
	return page, nil
}

func parseSearchCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

func omitEmptyAttrs(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func hostSearchHref(org, clusterID string, attrs map[string]string) string {
	params := url.Values{}
	params.Set("cluster", clusterID)
	if attrs["instance"] != "" {
		params.Set("hostIP", attrs["instance"])
	}
	if attrs["job"] != "" {
		params.Set("job", attrs["job"])
	}
	if attrs["host_name"] != "" {
		params.Set("host_name", attrs["host_name"])
	}
	return orgPrefixedSearch(org, "hosts/"+url.PathEscape(attrs["host_id"]), params)
}

func nodeSearchHref(org, clusterID, cluster, node string) string {
	params := url.Values{}
	params.Set("cluster", clusterID)
	if cluster != "" {
		params.Set("clusterName", cluster)
	}
	return orgPrefixedSearch(org, "k8s/nodes/"+url.PathEscape(node), params)
}

func clusterSearchHref(org, clusterID, cluster string) string {
	params := url.Values{}
	params.Set("cluster", clusterID)
	if cluster != "" {
		params.Set("clusterName", cluster)
	}
	return orgPrefixedSearch(org, "k8s", params)
}

func podSearchHref(org, clusterID string, attrs map[string]string) string {
	params := url.Values{}
	params.Set("cluster", clusterID)
	if attrs["cluster"] != "" {
		params.Set("clusterName", attrs["cluster"])
	}
	if attrs["namespace"] != "" {
		params.Set("namespace", attrs["namespace"])
	}
	if attrs["pod"] != "" {
		params.Set("pod", attrs["pod"])
	}
	return orgPrefixedSearch(org, "k8s/pod-details", params)
}

func orgPrefixedSearch(org, child string, params url.Values) string {
	if org == "" {
		return ""
	}
	path := "/v2/organizations/" + org + "/" + strings.TrimPrefix(child, "/")
	encoded := params.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
