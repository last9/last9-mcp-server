# Last9 MCP Server — Architecture

## Overview

The Last9 MCP Server exposes observability data (APM, metrics, logs, traces, alerts) and a persistent knowledge graph to AI agents via the Model Context Protocol (MCP). It runs as a Go binary, serving over STDIO (default) or HTTP, and provides 32 tools and 3 prompts organized into 7 subsystems.

```
┌──────────────────────────────────────────────────────────────┐
│                        MCP Client                            │
│              (Claude Code, Cursor, any MCP host)             │
└──────────────┬───────────────────────────────┬───────────────┘
               │  STDIO / HTTP                 │
┌──────────────▼───────────────────────────────▼───────────────┐
│                    Last9 MCP Server (Go)                      │
│                                                               │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌───────────┐ │
│  │    APM      │ │  Traces    │ │    Logs    │ │ Alerting  │ │
│  │  (9 tools)  │ │ (4 tools)  │ │ (5 tools)  │ │ (2 tools) │ │
│  └─────┬──────┘ └─────┬──────┘ └─────┬──────┘ └─────┬─────┘ │
│        │              │              │              │         │
│  ┌─────▼──────┐ ┌─────▼──────┐ ┌────▼───────┐              │
│  │  Change    │ │ Discovery  │ │ Knowledge  │              │
│  │  Events    │ │ (2 tools)  │ │   Graph    │              │
│  │ (1 tool)   │ │            │ │ (9 tools)  │              │
│  └─────┬──────┘ └─────┬──────┘ └─────┬──────┘              │
│        │              │              │                       │
│  ┌─────▼──────────────▼──────────────▼──────────────────┐   │
│  │              Last9 API  (HTTPS)                       │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │           SQLite Knowledge Store (~/.last9/)          │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              3 MCP Prompts (SRE Workflows)            │   │
│  └──────────────────────────────────────────────────────┘   │
└───────────────────────────────────────────────────────────────┘
```

## Startup Sequence

```
main()
  ├─ Load .env file
  ├─ Parse config (flags, env vars, config file via ff)
  ├─ Auth setup (TokenManager or DummyTokenManager for test/debug)
  ├─ Create Last9MCPServer
  ├─ registerAllTools(server, cfg) → returns knowledge.Store
  │   ├─ Register 23 API-backed tools (APM, Traces, Logs, Alerting, Change Events, Discovery)
  │   ├─ Initialize SQLite Knowledge Store (~/.last9/knowledge.db)
  │   ├─ Register 4 builtin schemas (upsert definitions, preserve service associations)
  │   ├─ Create extraction Pipeline
  │   └─ Register 9 Knowledge Graph tools
  ├─ registerAllPrompts(server, kStore)
  │   └─ Register 3 SRE workflow prompts
  └─ Serve (STDIO or HTTP)
```

---

## Tool Catalog

### 1. APM — Application Performance Monitoring (9 tools)

**Package**: `internal/apm/`

All APM tools call the Last9 API via authenticated HTTP. Prometheus tools additionally support per-query `datasource_name` override via a shared `resolveDatasourceCfg` helper.

| Tool | Purpose | Key Args |
|------|---------|----------|
| `get_service_summary` | Service landscape: throughput, error rate, p95 response time per service | `start_time_iso`, `end_time_iso`, `env` |
| `get_service_environments` | List available environments for filtering | `start_time_iso`, `end_time_iso` |
| `get_service_performance_details` | Deep service metrics: RED, apdex, availability, top operations, top errors | `service_name`, `start_time_iso`, `end_time_iso`, `env` |
| `get_service_operations_summary` | Per-operation breakdown: HTTP endpoints, DB queries, messaging, HTTP clients | `service_name`, `start_time_iso`, `end_time_iso`, `env` |
| `get_service_dependency_graph` | Incoming/outgoing services, databases, messaging — with RED metrics | `service_name`, `start_time_iso`, `end_time_iso`, `env` |
| `prometheus_range_query` | PromQL range query over a time window | `query`, `start_time_iso`, `end_time_iso`, `datasource_name` |
| `prometheus_instant_query` | PromQL instant query at a point in time | `query`, `time_iso`, `datasource_name` |
| `prometheus_label_values` | Label values for a given PromQL match query | `match_query`, `label`, `start_time_iso`, `end_time_iso`, `datasource_name` |
| `prometheus_labels` | List labels for a given PromQL match query | `match_query`, `start_time_iso`, `end_time_iso`, `datasource_name` |

### 2. Traces (4 tools)

**Package**: `internal/telemetry/traces/`

| Tool | Purpose | Key Args |
|------|---------|----------|
| `get_exceptions` | Server-side exceptions with stack traces, trace IDs, span attributes | `service_name`, `span_name`, `deployment_environment`, `lookback_minutes`, `limit` |
| `get_traces` | Query traces via JSON pipeline queries (filter, aggregate) | `tracejson_query`, `lookback_minutes`, `limit` |
| `get_service_traces` | Retrieve traces by trace ID or service name | `trace_id` OR `service_name`, `lookback_minutes`, `limit`, `env` |
| `get_trace_attributes` | Discover available trace attribute names for a time window | `lookback_minutes`, `start_time_iso`, `end_time_iso` |

### 3. Logs (5 tools)

**Package**: `internal/telemetry/logs/`

| Tool | Purpose | Key Args |
|------|---------|----------|
| `get_logs` | Advanced log queries via JSON pipeline (filter, parse, aggregate) | `logjson_query`, `lookback_minutes`, `limit` |
| `get_service_logs` | Raw log entries for a service with severity/body filters | `service`, `severity_filters[]`, `body_filters[]`, `lookback_minutes`, `limit`, `env` |
| `get_log_attributes` | Discover available log attribute names for a time window | `lookback_minutes`, `start_time_iso`, `end_time_iso` |
| `get_drop_rules` | List configured log drop rules | _(none)_ |
| `add_drop_rule` | Create a metadata-based log drop rule (equals/not_equals on attributes) | `name`, `filters[]` |

### 4. Alerting (2 tools)

**Package**: `internal/alerting/`

| Tool | Purpose | Key Args |
|------|---------|----------|
| `get_alert_config` | All configured alert rules with conditions, thresholds, labels | _(none)_ |
| `get_alerts` | Currently firing/recently fired alerts | `timestamp`, `window` (seconds) |

### 5. Change Events (1 tool)

**Package**: `internal/change_events/`

| Tool | Purpose | Key Args |
|------|---------|----------|
| `get_change_events` | Deployments, config changes, rollbacks, scaling events | `service`, `environment`, `event_name`, `lookback_minutes` |

### 6. Discovery (2 tools)

**Package**: `internal/discovery/`

| Tool | Purpose | Key Args |
|------|---------|----------|
| `discover_system_components` | System topology: pods, services, containers, namespaces, nodes, and their relationships | _(none)_ |
| `discover_metrics` | Available metrics and their labels | _(none)_ |

### 7. Knowledge Graph (9 tools)

**Package**: `internal/knowledge/`

Local SQLite-backed knowledge graph for persisting topology, metrics, and contextual notes across investigations.

| Tool | Purpose | Key Args |
|------|---------|----------|
| `ingest_knowledge` | Ingest nodes/edges/stats/events; or pass `raw_text` for auto-extraction | `nodes[]`, `edges[]`, `stats[]`, `events[]`, `raw_text` |
| `search_knowledge_graph` | Full-text search across nodes, notes | `query`, `limit` |
| `define_knowledge_schema` | Define custom architectural schemas (builtin schemas are immutable) | `name`, `blueprint` |
| `list_knowledge_schemas` | List all schemas (4 builtin + user-defined) | _(none)_ |
| `add_service_to_schema` | Associate a service with an architectural pattern | `schema_name`, `service` |
| `remove_service_from_schema` | Disassociate a service from a schema | `schema_name`, `service` |
| `add_knowledge_note` | Create a titled markdown note linked to nodes/edges | `title`, `body`, `node_ids[]`, `edge_refs[]` |
| `get_knowledge_note` | Retrieve full note body and linked entities | `id` |
| `delete_knowledge_note` | Permanently remove a note | `id` |

---

## Knowledge Graph Subsystem

### Data Model

```
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│    Node      │────▶│    Edge      │     │  Statistic   │
│  id (PK)     │     │  source_id   │     │  node_id     │
│  type        │     │  target_id   │     │  metric_name │
│  name        │     │  relation    │     │  value       │
│  env         │     │  properties  │     │  unit        │
│  properties  │     └──────────────┘     │  timestamp   │
└──────┬──────┘                           └──────────────┘
       │
       │  linked via                      ┌──────────────┐
       │                                  │    Event      │
┌──────▼──────┐     ┌─────────────┐      │  source_id   │
│    Note      │     │   Schema     │      │  type        │
│  id (PK)     │     │  name (PK)   │      │  severity    │
│  title       │     │  description │      │  count       │
│  body (md)   │     │  builtin     │      │  window      │
│  created_at  │     │  blueprint   │      └──────────────┘
└──────────────┘     │  services[]  │
                     └──────────────┘
```

**Storage**: SQLite via `modernc.org/sqlite` (pure Go, no CGO). Default path: `~/.last9/knowledge.db`.

**FTS5**: External-content FTS5 table on `nodes` (name, type, env) and `notes` (title, body) for full-text search. Triggers keep FTS in sync on INSERT/UPDATE/DELETE.

**UPSERT Semantics**: Nodes use `INSERT ... ON CONFLICT DO UPDATE` with `COALESCE(excluded.env, nodes.env)` so extractors without env data don't overwrite existing values.

### Extraction Pipeline

When `ingest_knowledge` receives `raw_text`, the Pipeline auto-parses tool outputs into graph entities:

```
raw_text
  │
  ▼
Format Detection (JSON > YAML > CSV > PlainText)
  │
  ├─ Structured (JSON/YAML) ──▶ ExtractorRegistry (5 extractors, priority-ordered)
  │   │
  │   ├─ ComponentDiscoveryExtractor   ← discover_system_components output
  │   ├─ DependencyGraphExtractor      ← get_service_dependency_graph output
  │   ├─ OperationsSummaryExtractor    ← get_service_operations_summary output
  │   ├─ ServiceSummaryExtractor       ← get_service_summary output
  │   └─ PrometheusExtractor           ← prometheus query output
  │
  └─ PlainText ──▶ Drain log template miner (fallback)
  │
  ▼
ExtractionResult {Nodes, Edges, Stats, Confidence, Pattern}
  │
  ▼
Schema Matching: score = 0.5·EdgeCoverage + 0.3·NodeCoverage + 0.2·FieldConfidence
  │
  ▼
SQLite Storage
```

### Prometheus Extractor Detail

The most sophisticated extractor. Converts Prometheus metric labels into graph topology:

**Entity Rules** — map labels to node types via canonical names with aliases:

| Canonical Label | Aliases | Node Type | Scope Labels | Priority |
|----------------|---------|-----------|-------------|----------|
| `namespace` | `namespace`, `k8s_namespace_name` | Namespace | — | 1 |
| `deployment` | `deployment`, `k8s_deployment_name` | Deployment | namespace | 2 |
| `service` | `service`, `service_name` | Service | namespace | 2 |
| `pod` | `pod`, `k8s_pod_name` | Pod | namespace | 3 |
| `container` | `container`, `k8s_container_name` | Container | namespace, pod | 4 |
| `node` | `node`, `k8s_node_name` | VirtualMachine | — | 1 |
| `instance` | `instance` | VirtualMachine | — | 1 (node_ prefix only) |
| `topic` | `topic`, `redpanda_topic` | KafkaTopic | — | 2 |
| `consumergroup` | `consumergroup`, `redpanda_group` | KafkaConsumerGroup | — | 2 |

**Edge Rules** — inferred from label co-occurrence:

| Source Label | Target Label | Relation |
|-------------|-------------|----------|
| namespace | deployment | CONTAINS |
| namespace | service | CONTAINS |
| namespace | pod | CONTAINS |
| deployment | pod | MANAGES |
| pod | container | RUNS |
| pod | node | RUNS_ON |
| consumergroup | topic | CONSUMES_FROM |

**Environment Resolution** — priority-ordered metadata enrichment (not entity creation):
`environment` > `env` > `cluster`

**Stat Attachment** — metrics attach to the highest-priority entity in the series (Container > Pod > Deployment/Service > Namespace/VM). Metric names are qualified with resource labels (e.g. `kube_pod_container_resource_requests:cpu`).

### Builtin Schemas

4 architectural patterns embedded via `//go:embed`:

| Schema | Pattern | Key Node Types |
|--------|---------|----------------|
| `http_k8s_datastore` | HTTP services on Kubernetes with datastores | Service, HTTPEndpoint, Pod, Container, Namespace, DataStoreInstance |
| `http_vm_datastore` | HTTP services on VMs with datastores | VirtualMachine, ServiceProcess, HTTPEndpoint, DataStoreInstance |
| `kafka_consumer_jobs` | Kafka/Redpanda consumers writing to datastores | ConsumerJob, KafkaTopic, KafkaConsumerGroup, DataStoreInstance |
| `ingest_gateway` | HTTP ingest services producing to Kafka/Redpanda | IngestService, KafkaBroker, KafkaTopic |

Builtin schemas are immutable via `define_knowledge_schema` — service associations managed through `add_service_to_schema` / `remove_service_from_schema`.

---

## MCP Prompts

3 SRE workflow prompts available to any MCP client. Each is a multi-step investigation guide with embedded reference material and optional knowledge graph pre-loading.

### Prompt Architecture

```
prompts/
├── workflows/           ← Investigation steps (user role message)
│   ├── k8s-infra-analysis.md
│   ├── app-performance-analysis.md
│   └── incident-rca.md
└── references/          ← Shared reference material (assistant role message)
    ├── investigation-framework.md
    ├── prometheus-k8s-queries.md
    ├── apm-tool-patterns.md
    └── rca-note-template.md
```

Files are embedded at compile time via `//go:embed`. When a prompt is invoked:

1. If `service_name` is provided, query the knowledge graph for prior context → prepend as assistant message
2. Concatenate the prompt's reference files → send as assistant message
3. Substitute argument placeholders (`$SERVICE_NAME`, `$ENVIRONMENT`, etc.) in workflow → send as user message

### Prompt Definitions

| Prompt | Purpose | Arguments | References |
|--------|---------|-----------|------------|
| `k8s-infra-analysis` | Kubernetes infrastructure issues: pod crashes, OOM, node pressure, HPA, scheduling | `service_name`, `namespace`, `environment` | investigation-framework, prometheus-k8s-queries |
| `app-performance-analysis` | Application performance: slow endpoints, error spikes, DB issues, messaging lag | `service_name`, `environment`, `start_time`, `end_time` | investigation-framework, apm-tool-patterns |
| `incident-rca` | Root cause analysis: availability drops, latency spikes, SLO breaches | `service_name`, `environment`, `start_time`, `end_time` | investigation-framework, apm-tool-patterns, rca-note-template |

### Workflow Design Principles

All workflows share these conventions (defined in `investigation-framework.md`):

**Tool Selection Priority** (7 tiers):
1. Knowledge Graph — prior RCAs, ownership, known issues
2. APM tools — service summary, performance details, operations, dependency graph
3. Alerts & Change Events — currently firing alerts, recent deployments
4. Traces — exceptions, distributed traces
5. Prometheus — infrastructure metrics (PromQL)
6. kubectl — Kubernetes state (last resort for infra)
7. Logs — raw logs (absolute last resort, targeted filters only)

**Knowledge Graph Discipline**:
- Search KG at the start of every investigation
- Ingest discovered topology via `ingest_knowledge`
- Record findings via `add_knowledge_note` at the end (mandatory for RCA)
- Use deterministic node IDs (`service:frontend`, `pod:default:nginx-abc`)

**Confirm Before Concluding**: Every workflow includes an explicit "present findings and ask clarifying questions" step before making recommendations.

---

## Package Structure

```
last9-mcp-server/
├── main.go                          ← Entry point, config, server setup
├── tools.go                         ← Registers all 32 MCP tools
├── prompts.go                       ← Registers all 3 MCP prompts
├── http_server.go                   ← HTTP transport (optional)
├── prompts/
│   ├── workflows/*.md               ← 3 investigation workflows
│   └── references/*.md              ← 4 shared reference files
├── internal/
│   ├── models/config.go             ← Server configuration struct
│   ├── auth/                        ← Token management, HTTP client
│   ├── utils/                       ← API config population, datasource resolution
│   ├── apm/                         ← 9 APM tool handlers + args
│   ├── telemetry/
│   │   ├── traces/                  ← 4 trace tool handlers + args
│   │   └── logs/                    ← 5 log tool handlers + args
│   ├── alerting/                    ← 2 alerting tool handlers + args
│   ├── change_events/               ← 1 change events tool handler + args
│   ├── discovery/                   ← 2 discovery tool handlers + args
│   └── knowledge/                   ← Knowledge graph subsystem
│       ├── models.go                ← Node, Edge, Statistic, Event, Schema, Note
│       ├── store.go                 ← Store interface + SQLite implementation
│       ├── tools.go                 ← 9 KG tool handlers + args + descriptions
│       ├── extract.go               ← Pipeline, ExtractorRegistry, MatchSchemasScored
│       ├── extract_depgraph.go      ← DependencyGraphExtractor
│       ├── extract_components.go    ← ComponentDiscoveryExtractor
│       ├── extract_summary.go       ← ServiceSummaryExtractor
│       ├── extract_operations.go    ← OperationsSummaryExtractor
│       ├── extract_prometheus.go    ← PrometheusExtractor
│       ├── prom_rules.go            ← Entity/edge rules, label aliases, resolveEnv
│       ├── format.go                ← Format detection (JSON/YAML/CSV/PlainText)
│       ├── fieldmatch.go            ← Token similarity, MakeNodeID, field aliases
│       ├── drain.go                 ← Drain log template miner
│       ├── schema.go                ← Schema signature, MatchSchemas
│       ├── builtin.go               ← Load/register embedded YAML schemas
│       └── schemas/*.yaml           ← 4 builtin architectural schemas
└── .claude/commands/                ← Claude Code skills (offline counterparts of prompts)
```

## Handler Pattern

All MCP tool handlers follow the same typed pattern via the `mcp-go-sdk`:

```go
func NewXxxHandler(client *http.Client, cfg models.Config) last9mcp.TypedHandler[XxxArgs] {
    return func(ctx context.Context, req *mcp.CallToolRequest, args XxxArgs) (*mcp.CallToolResult, any, error) {
        // 1. Validate/default args
        // 2. Call Last9 API or local store
        // 3. Return CallToolResult with JSON content
    }
}
```

The `RegisterInstrumentedTool` wrapper adds OpenTelemetry tracing and rate limiting around each handler.

## Authentication

- **Production**: `LAST9_REFRESH_TOKEN` → `TokenManager` exchanges for access tokens, auto-refreshes
- **Test/Debug**: `LAST9_ENV=test` → `DummyTokenManager`, no auth, configurable API host
- HTTP client injected into all API-backed handlers

## Transport

- **STDIO** (default): `mcp.StdioTransport{}` — used by Claude Code, IDE integrations
- **HTTP**: `--http` flag → SSE-based HTTP server on configurable host:port
