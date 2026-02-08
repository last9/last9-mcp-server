# Prometheus K8s Query Catalog

PromQL queries for Kubernetes infrastructure analysis. Replace `$POD`, `$NS`, `$DEPLOY`, `$NODE`, `$CONTAINER`, `$PVC` with actual values. Use `discover_metrics` first if unsure which metrics exist in the environment.

## CPU

### CPU Usage vs Requests
```promql
# Per-pod CPU usage rate
sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# CPU requests per pod
sum by (pod) (kube_pod_container_resource_requests{namespace="$NS", pod=~"$POD.*", resource="cpu"})

# CPU limits per pod
sum by (pod) (kube_pod_container_resource_limits{namespace="$NS", pod=~"$POD.*", resource="cpu"})

# CPU utilization ratio (usage / request)
sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="$NS", pod=~"$POD.*"}[5m]))
  / sum by (pod) (kube_pod_container_resource_requests{namespace="$NS", pod=~"$POD.*", resource="cpu"})
```

### CPU Throttling
```promql
# Throttled periods ratio — above 25% indicates resource starvation
sum by (pod) (rate(container_cpu_cfs_throttled_periods_total{namespace="$NS", pod=~"$POD.*"}[5m]))
  / sum by (pod) (rate(container_cpu_cfs_periods_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# Total throttled seconds
sum by (pod) (rate(container_cpu_cfs_throttled_seconds_total{namespace="$NS", pod=~"$POD.*"}[5m]))
```

### Node-Level CPU
```promql
# Node CPU utilization
1 - avg by (node) (rate(node_cpu_seconds_total{mode="idle", node="$NODE"}[5m]))

# System vs user CPU
sum by (node, mode) (rate(node_cpu_seconds_total{node="$NODE", mode=~"system|user|iowait"}[5m]))
```

## Memory

### Memory Usage vs Requests
```promql
# Working set (what matters for OOM)
sum by (pod) (container_memory_working_set_bytes{namespace="$NS", pod=~"$POD.*", container!=""})

# RSS
sum by (pod) (container_memory_rss{namespace="$NS", pod=~"$POD.*", container!=""})

# Memory requests
sum by (pod) (kube_pod_container_resource_requests{namespace="$NS", pod=~"$POD.*", resource="memory"})

# Memory limits
sum by (pod) (kube_pod_container_resource_limits{namespace="$NS", pod=~"$POD.*", resource="memory"})

# Memory utilization ratio (working set / limit) — OOM kills at ~100%
sum by (pod) (container_memory_working_set_bytes{namespace="$NS", pod=~"$POD.*", container!=""})
  / sum by (pod) (kube_pod_container_resource_limits{namespace="$NS", pod=~"$POD.*", resource="memory"})
```

### OOM Kills
```promql
# OOM kill count (KSM)
kube_pod_container_status_last_terminated_reason{namespace="$NS", pod=~"$POD.*", reason="OOMKilled"}

# Container restart count
sum by (pod, container) (kube_pod_container_status_restarts_total{namespace="$NS", pod=~"$POD.*"})

# Rate of restarts (detect restart loops)
sum by (pod, container) (rate(kube_pod_container_status_restarts_total{namespace="$NS", pod=~"$POD.*"}[1h]))
```

### Node-Level Memory
```promql
# Node memory available
node_memory_MemAvailable_bytes{node="$NODE"}

# Node memory pressure (available / total)
node_memory_MemAvailable_bytes{node="$NODE"} / node_memory_MemTotal_bytes{node="$NODE"}
```

## Disk and PVC / IOPS

### PVC Usage
```promql
# PVC used bytes
kubelet_volume_stats_used_bytes{namespace="$NS", persistentvolumeclaim="$PVC"}

# PVC capacity
kubelet_volume_stats_capacity_bytes{namespace="$NS", persistentvolumeclaim="$PVC"}

# PVC utilization ratio
kubelet_volume_stats_used_bytes{namespace="$NS", persistentvolumeclaim="$PVC"}
  / kubelet_volume_stats_capacity_bytes{namespace="$NS", persistentvolumeclaim="$PVC"}

# PVC inodes used vs available
kubelet_volume_stats_inodes_used{namespace="$NS", persistentvolumeclaim="$PVC"}
  / kubelet_volume_stats_inodes{namespace="$NS", persistentvolumeclaim="$PVC"}
```

### Disk IOPS (Node-Level)
```promql
# Read IOPS
sum by (node) (rate(node_disk_reads_completed_total{node="$NODE"}[5m]))

# Write IOPS
sum by (node) (rate(node_disk_writes_completed_total{node="$NODE"}[5m]))

# Read throughput (bytes/s)
sum by (node) (rate(node_disk_read_bytes_total{node="$NODE"}[5m]))

# Write throughput (bytes/s)
sum by (node) (rate(node_disk_written_bytes_total{node="$NODE"}[5m]))

# IO wait time (saturation indicator)
sum by (node) (rate(node_disk_io_time_seconds_total{node="$NODE"}[5m]))
```

## Network

### Pod/Container Network
```promql
# Receive bandwidth
sum by (pod) (rate(container_network_receive_bytes_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# Transmit bandwidth
sum by (pod) (rate(container_network_transmit_bytes_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# Receive errors
sum by (pod) (rate(container_network_receive_errors_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# Transmit errors
sum by (pod) (rate(container_network_transmit_errors_total{namespace="$NS", pod=~"$POD.*"}[5m]))

# Dropped packets (receive)
sum by (pod) (rate(container_network_receive_packets_dropped_total{namespace="$NS", pod=~"$POD.*"}[5m]))
```

### Node-Level Network
```promql
# Node receive bandwidth
sum by (node) (rate(node_network_receive_bytes_total{node="$NODE", device!="lo"}[5m]))

# Node transmit bandwidth
sum by (node) (rate(node_network_transmit_bytes_total{node="$NODE", device!="lo"}[5m]))

# Conntrack table utilization (networking issues when full)
node_nf_conntrack_entries{node="$NODE"} / node_nf_conntrack_entries_limit{node="$NODE"}
```

## HPA (Horizontal Pod Autoscaler)

```promql
# Current replicas vs desired
kube_horizontalpodautoscaler_status_current_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}
kube_horizontalpodautoscaler_status_desired_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}

# HPA min/max config
kube_horizontalpodautoscaler_spec_min_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}
kube_horizontalpodautoscaler_spec_max_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}

# HPA target utilization vs actual
kube_horizontalpodautoscaler_spec_target_metric{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}

# HPA scaling limited (at max replicas)
kube_horizontalpodautoscaler_status_current_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}
  == kube_horizontalpodautoscaler_spec_max_replicas{namespace="$NS", horizontalpodautoscaler="$DEPLOY"}

# Scaling condition
kube_horizontalpodautoscaler_status_condition{namespace="$NS", horizontalpodautoscaler="$DEPLOY", condition="ScalingLimited", status="true"}
```

## Pod Scheduling and Conditions

```promql
# Pods not ready
kube_pod_status_ready{namespace="$NS", condition="false"}

# Pods in pending state
kube_pod_status_phase{namespace="$NS", phase="Pending"}

# Pods in CrashLoopBackOff
kube_pod_container_status_waiting_reason{namespace="$NS", reason="CrashLoopBackOff"}

# Pods unschedulable
kube_pod_status_unschedulable{namespace="$NS"}

# Container waiting reasons (ImagePullBackOff, ErrImagePull, CreateContainerConfigError)
kube_pod_container_status_waiting_reason{namespace="$NS", pod=~"$POD.*"}
```

## Node Conditions and Pressure

```promql
# Node conditions (Ready, MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable)
kube_node_status_condition{node="$NODE", condition="Ready", status="true"}
kube_node_status_condition{node="$NODE", condition="MemoryPressure", status="true"}
kube_node_status_condition{node="$NODE", condition="DiskPressure", status="true"}
kube_node_status_condition{node="$NODE", condition="PIDPressure", status="true"}

# Node allocatable vs capacity
kube_node_status_allocatable{node="$NODE", resource="cpu"}
kube_node_status_allocatable{node="$NODE", resource="memory"}
kube_node_status_capacity{node="$NODE", resource="pods"}

# Pods per node vs capacity
count by (node) (kube_pod_info{node="$NODE"})
  / kube_node_status_capacity{node="$NODE", resource="pods"}

# Node cordoned/unschedulable
kube_node_spec_unschedulable{node="$NODE"}
```

## Deployment and ReplicaSet Status

```promql
# Deployment replicas status
kube_deployment_status_replicas{namespace="$NS", deployment="$DEPLOY"}
kube_deployment_status_replicas_available{namespace="$NS", deployment="$DEPLOY"}
kube_deployment_status_replicas_unavailable{namespace="$NS", deployment="$DEPLOY"}
kube_deployment_status_replicas_updated{namespace="$NS", deployment="$DEPLOY"}

# Rollout stuck (unavailable > 0 for extended period)
kube_deployment_status_replicas_unavailable{namespace="$NS", deployment="$DEPLOY"} > 0

# Deployment generation mismatch (rollout in progress)
kube_deployment_metadata_generation{namespace="$NS", deployment="$DEPLOY"}
  != kube_deployment_status_observed_generation{namespace="$NS", deployment="$DEPLOY"}
```
