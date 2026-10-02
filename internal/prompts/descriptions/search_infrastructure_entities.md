Lists hosts, Kubernetes clusters, nodes, or pods from live inventory.

Use this to find a typed entity id before calling get_infrastructure_context.
Does not join Host to Node — that is get_infrastructure_context. Does not require
writing PromQL.

Parameters:
- entity_type: (Required) host, k8s_cluster, k8s_node, or k8s_pod.
- query: (Optional) Substring filter on the entity name. Regex metacharacters are literal.
- limit: (Optional) Page size (default 25, max 100).
- cursor: (Optional) next_cursor from the previous page.
- timestamp: (Optional) Unix seconds. Defaults to now.
- cluster_id: (Optional) Levitate datasource UUID used in returned ids. Defaults to the configured datasource.

Returns typed ids (host:..., k8s-node:..., k8s-cluster:..., k8s-pod:...), key attributes,
dashboard hrefs, and next_cursor when more pages exist. Then call get_infrastructure_context
with those selectors.
