Resolves one host, Kubernetes node, or Kubernetes pod to its depth-one infrastructure neighbors.

Use this instead of writing PromQL that joins node_uname_info to kube_node_info.
Returns the same identity the Last9 Host / Node / Pod UI uses: typed entity id, match
status (matched, not_found, stale, ambiguous), observed edges (scheduled_on, member_of,
same_machine), evidence labels, and a dashboard href.

When to use:
- A host page question needs the Kubernetes node or cluster it maps to
- A node or pod question needs the underlying host
- Ambiguous matches: present candidates; never pick the first silently

Parameters:
- entity_type: (Required) host, k8s_node, or k8s_pod. k8s_cluster is not valid input.
- selectors: (Required) Identity fields for that type:
  - host: instance, host_id, or host_name (optional job)
  - k8s_node: cluster and node
  - k8s_pod: cluster plus uid, or cluster plus namespace and pod
- timestamp: (Optional) Unix seconds. Defaults to now.
- cluster_id: (Optional) Levitate datasource UUID. Defaults to the configured datasource.
- region: (Optional) AWS region header. Defaults to the configured datasource region.

Do not send Prometheus credentials. Call did_you_mean with type=host or type=k8s_node
when the name may be misspelled.
