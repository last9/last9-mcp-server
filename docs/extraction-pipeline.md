# Format-Aware Extraction Pipeline for Knowledge Graph Ingestion

## Overview

The extraction pipeline converts raw tool outputs (JSON, YAML, CSV) and unstructured text into knowledge graph elements (nodes, edges, statistics) that can be stored and queried. It sits between the `ingest_knowledge` MCP tool's `raw_text` input and the graph store, replacing the previous Drain-only path.

The fundamental problem: observability tools return structured data in varied shapes (dependency graphs, component lists, service summaries, Prometheus metrics). Without the pipeline, an LLM agent would need to manually parse each tool's output and construct `nodes`/`edges` arrays on every call. The pipeline automates this mapping for known tool output shapes, falling back to agent-driven parsing only for truly unrecognized formats.

```
                        ingest_knowledge(raw_text="...")
                                    |
                                    v
                        +-----------------------+
                        |   Format Detection    |
                        |  (JSON > YAML > CSV   |
                        |   > PlainText)        |
                        +-----------+-----------+
                                    |
                    +---------------+---------------+
                    |                               |
            Structured (JSON/YAML)          PlainText
                    |                               |
                    v                               v
        +-------------------+              +-------------+
        | Extractor Registry|              | DrainTree   |
        | (priority-ordered)|              | (log lines) |
        +-------------------+              +------+------+
        | 1. ComponentDisc. |                     |
        | 2. DepGraph       |              Template match?
        | 3. OpsSummary     |              + mapping rule?
        | 4. SvcSummary     |                     |
        | 5. Prometheus     |              Yes: nodes/edges
        +--------+----------+              No: error → agent
                 |
          CanHandle? → Extract
                 |
                 v
        +-------------------+
        | ExtractionResult  |
        | {Nodes, Edges,    |
        |  Stats, Confidence|
        |  Pattern}         |
        +--------+----------+
                 |
                 v
        +-------------------+
        | MatchSchemasScored|
        | (weighted scoring)|
        +--------+----------+
                 |
                 v
        +-------------------+
        |    Store Ingest   |
        | (nodes, edges,    |
        |  stats, events)   |
        +-------------------+
```

## Stage 1: Format Detection

**File**: `format.go`

The first stage determines what kind of data we're looking at. Detection is ordered from most specific to least specific, and the first match wins.

### Detection Order

| Priority | Check | Result |
|----------|-------|--------|
| 1 | Empty/whitespace-only input | `FormatUnknown` (no processing) |
| 2 | Starts with `{` or `[` and `json.Unmarshal` succeeds | `FormatJSON` |
| 3 | `yaml.Unmarshal` succeeds and result is map or slice (not bare string) | `FormatYAML` |
| 4 | First line has commas, >=3 lines, consistent field count per line | `FormatCSV` |
| 5 | Default | `FormatPlainText` |

**Why JSON before YAML**: YAML is a superset of JSON. Every valid JSON document is also valid YAML. If we tried YAML first, JSON inputs would be parsed as YAML maps, losing the ability to distinguish the two. Since most tool outputs are JSON, checking JSON first is both correct and efficient.

**Why YAML excludes bare strings**: The string `"hello"` is valid YAML (it parses to a `string`), but we don't want to treat arbitrary text as YAML. Only maps and slices indicate actual structured data.

**Why CSV requires >= 3 lines**: Two lines (header + one row) could easily be coincidental comma-separated text. Three lines with consistent field counts is a stronger signal.

### Return Value

`DetectFormat` returns `(FormatType, interface{}, error)`. The `interface{}` contains the parsed representation:
- JSON: the result of `json.Unmarshal` — a `map[string]interface{}` or `[]interface{}`
- YAML: the result of `yaml.Unmarshal` — same types
- CSV: `[][]string` from `csv.Reader.ReadAll()`
- PlainText: `nil`

This parsed value is passed directly to extractors, avoiding redundant re-parsing.

## Stage 2: Structural Extraction

**Files**: `extract.go`, `extract_depgraph.go`, `extract_components.go`, `extract_summary.go`, `extract_operations.go`, `extract_prometheus.go`

For structured data (JSON/YAML), the pipeline tries a registry of pattern-specific extractors. Each extractor recognizes a specific tool output shape and converts it to graph elements.

### The Extractor Interface

```go
type Extractor interface {
    Name() string                                        // Human-readable ID for diagnostics
    CanHandle(parsed interface{}) bool                   // Does this look like my pattern?
    Extract(parsed interface{}) (*ExtractionResult, error) // Convert to graph elements
}
```

The two-method design (CanHandle + Extract) separates recognition from transformation. `CanHandle` is a cheap structural check (does the top-level object have these specific keys?). `Extract` does the actual work of creating nodes, edges, and statistics.

### Dispatch Strategy

The `ExtractorRegistry` holds extractors in priority order. `TryExtract` iterates them and returns the result from the first extractor whose `CanHandle` returns true.

**Why first-match, not best-match**: The five tool output shapes are sufficiently distinct that their `CanHandle` checks don't overlap. Running all extractors and picking the best confidence score would be slower for no practical benefit. If a future tool output were ambiguous between two extractors, the priority order breaks the tie predictably.

### Registration Order (most specific first)

1. **ComponentDiscoveryExtractor** — requires both `components` (map) AND `triples` (array)
2. **DependencyGraphExtractor** — requires `service_name` AND (`incoming` OR `outgoing` OR `databases`)
3. **OperationsSummaryExtractor** — requires `service_name` AND `operations` (array)
4. **ServiceSummaryExtractor** — requires all top-level values to be objects with `ServiceName`/`Throughput`
5. **PrometheusExtractor** — requires array where elements have `metric` AND (`value` OR `values`)

ComponentDiscovery is first because its two-key check (`components` + `triples`) is the most distinctive. DependencyGraph and OperationsSummary both check for `service_name`, but DependencyGraph additionally checks for `incoming`/`outgoing`/`databases` which OperationsSummary doesn't have — the ordering between them doesn't matter because their `CanHandle` checks are mutually exclusive. Prometheus is last because it matches any array of metric objects, which is the broadest pattern.

### Pattern A: Dependency Graph (`extract_depgraph.go`)

**Tool**: `get_service_dependency_graph`

**Input shape**:
```json
{
  "service_name": "api-service",
  "incoming": {"web-frontend": {"Throughput": 150.5, "ErrorRate": 2.3}},
  "outgoing": {"database-service": {"Throughput": 200.0}},
  "databases": {"postgres-primary": {"Throughput": 300.0}},
  "messaging_systems": {"kafka-cluster": {"Throughput": 500.0}}
}
```

**CanHandle**: Top-level object has `service_name` AND at least one of `incoming`, `outgoing`, `databases`.

**Extraction logic**:
1. Create a root Service node for `service_name`
2. For each key in `incoming`: create a Service node, add a `CALLS` edge from that service to the root
3. For each key in `outgoing`: create a Service node, add a `CALLS` edge from root to that service
4. For each key in `databases`: create a DataStoreInstance node, add `CONNECTS_TO` edge from root
5. For each key in `messaging_systems`: create a KafkaTopic node, add `PRODUCES_TO` edge from root
6. For each entry's metrics map, extract RED metrics (Throughput, ResponseTimeP50/P90/P95/Avg, ErrorRate, ErrorPercent) as Statistics on the relationship's target node

**Node IDs**: Deterministic via `MakeNodeID` — e.g., `service:api-service`, `datastoreinstance:postgres-primary`.

**Edge semantics**: The direction of `CALLS` edges reflects the actual call direction. Incoming services call us; we call outgoing services. This matches the schema patterns like `Service -> CALLS -> Service`.

### Pattern B: Component Discovery (`extract_components.go`)

**Tool**: `discover_system_components`

**Input shape**:
```json
{
  "components": {
    "POD": ["pod-1", "pod-2"],
    "SERVICE": ["service-1"],
    "CONTAINER": ["container-1"]
  },
  "triples": [
    {"src": "service-1", "rel": "HAS_ENDPOINTS", "dst": "pod-1"},
    {"src": "default", "rel": "CONTAINS", "dst": "service-1"}
  ]
}
```

**CanHandle**: Top-level object has `components` (must be a map) AND `triples` (must be an array).

**Extraction logic**:
1. For each key in `components`, normalize the type name (POD→Pod, SERVICE→Service, CONTAINER→Container, NAMESPACE→Namespace, DEPLOYMENT→Deployment, NODE→Node) and create a node per list entry
2. For each triple, resolve `src` and `dst` to node IDs by searching the node set by name suffix. If a source/destination can't be resolved, create an "Unknown" type node for it
3. Create edges using the triple's `rel` field as the relation

**Type normalization**: The discovery API uses uppercase keys (POD, SERVICE) while the knowledge graph uses PascalCase (Pod, Service). The `typeNormalization` map handles the five known types; unknown types get title-cased as a fallback.

**Name-to-ID resolution**: Triples reference components by name (e.g., "pod-1"), but node IDs are type-prefixed (e.g., "pod:pod-1"). The `resolveComponentNodeID` function searches the node set for an ID ending with `:name`. This is a linear scan over the node set, which is acceptable because component lists are typically small (hundreds of entries, not millions).

### Pattern C: Service Summary (`extract_summary.go`)

**Tool**: `get_service_summary`

**Input shape**:
```json
{
  "svc1": {"ServiceName": "svc1", "Throughput": 10.5, "ErrorRate": 0.5, "ResponseTime": 2.3},
  "svc2": {"ServiceName": "svc2", "Throughput": 20.0, "ErrorRate": 1.0, "ResponseTime": 5.1}
}
```

**CanHandle**: Top-level object where (a) it's non-empty, (b) ALL values are objects, and (c) every object contains `ServiceName` or `Throughput`. The all-values-must-match requirement prevents false positives on objects that happen to have one matching key.

**Extraction logic**:
1. For each key-value pair, create a Service node. Use `ServiceName` from the inner object if available, otherwise use the map key
2. Extract Throughput (req/s), ErrorRate (errors/s), and ResponseTime (ms) as Statistics on the service node

**No edges**: Service summary is a flat list with no relationship information. Edges come from other tool outputs (dependency graph, operations summary) that are ingested separately. The same service node IDs will merge naturally due to deterministic ID generation.

### Pattern D: Operations Summary (`extract_operations.go`)

**Tool**: `get_service_operations_summary`

**Input shape**:
```json
{
  "service_name": "payment-service",
  "operations": [
    {
      "name": "ProcessPayment",
      "db_system": "postgresql",
      "net_peer_name": "db.internal",
      "messaging_system": "kafka",
      "throughput": 150.5,
      "error_rate": 2.3,
      "response_time": {"p50": 25.5, "p95": 45.2}
    }
  ]
}
```

**CanHandle**: Top-level object has `service_name` (string) AND `operations` (array). Distinguished from the dependency graph extractor because the dependency graph has `incoming`/`outgoing`/`databases` keys, not `operations`.

**Extraction logic**:
1. Create a root Service node for `service_name`
2. For each operation:
   - Create an HTTPEndpoint node with ID `httpendpoint:<service>:<operation-name>`
   - Add `EXPOSES` edge from Service to HTTPEndpoint
   - If `db_system` is non-empty: create DataStoreInstance node (ID includes `net_peer_name` if available for specificity), add `CONNECTS_TO` edge from endpoint to datastore
   - If `messaging_system` is non-empty: create KafkaTopic node, add `PRODUCES_TO` edge from endpoint to topic
   - Extract throughput, error_rate, error_percent as Statistics on the endpoint node
   - Extract nested `response_time` map entries (p50, p95, etc.) as separate Statistics with "response_time.p50" etc. naming

**Endpoint node IDs**: Include both service name and operation name — `httpendpoint:payment-service:ProcessPayment` — to ensure uniqueness across services while enabling merging when the same endpoint appears in multiple ingestions.

### Pattern E: Prometheus Results (`extract_prometheus.go`)

**Tools**: `prometheus_instant_query`, `prometheus_range_query`

**Instant query input shape**:
```json
[{"metric": {"__name__": "up", "service_name": "api"}, "value": [1700000000, "1"]}]
```

**Range query input shape**:
```json
[{"metric": {"__name__": "cpu_usage", "job": "node-exporter"}, "values": [[1700000000, "0.5"], [1700000060, "0.7"]]}]
```

**CanHandle**: Input is an array where the first element has a `metric` (map) key AND either `value` or `values`.

**Extraction logic**:
1. For each array element, extract the metric labels and the metric name (from `__name__` label)
2. Resolve the node ID from labels with this priority: `service_name` > `service` > `job` > `instance`. If none found, use `metric:<name>` as a fallback
3. For instant queries (`value` field): parse the second element of the [timestamp, value] pair
4. For range queries (`values` field): use the most recent value (last element in the array)
5. Create a Statistic per metric series

**Statistics only, no topology**: Prometheus metrics don't inherently describe relationships between components. The node ID resolution maps metrics back to service nodes that were (or will be) created by other extractors. This design allows metrics to be ingested independently and attached to the correct nodes.

**String value parsing**: Prometheus instant queries return values as strings (e.g., `"42.5"` not `42.5`). The `parsePrometheusValue` function handles both string and float64 representations.

### ExtractionResult

Every extractor returns the same result type:

```go
type ExtractionResult struct {
    Nodes      []Node      // Graph nodes to ingest
    Edges      []Edge      // Graph edges to ingest
    Stats      []Statistic // Ephemeral metrics to ingest
    Confidence float64     // 0.0..1.0 — how well the extractor matched
    Pattern    string      // Extractor name, set by registry after extraction
}
```

**Confidence scoring**: Each extractor assigns a confidence based on how much useful data it extracted:
- 0.85-0.9: Normal extraction with meaningful data
- 0.2-0.3: Matched the shape but data was empty or minimal
- 0.1: Edge case (e.g., empty service name)

Confidence is informational in the current implementation — it's not used for extractor selection (first-match wins). It could be used for future best-match dispatch or for reporting to the agent.

## Stage 3: Plain Text Fallback (Drain)

**File**: `drain.go`, called from `extract.go`

When format detection returns `FormatPlainText`, the pipeline delegates to the Drain log template miner. Drain is designed for clustering natural-language log lines like `"Connection to db:postgres failed"` — it tokenizes by whitespace and discovers recurring patterns by generalizing variable positions to `<*>` wildcards.

### How Drain Works

Drain maintains a tree structure indexed by:
1. **Token count** (first level) — log lines of different lengths go to different subtrees
2. **Token values** (subsequent levels) — exact token matches navigate the tree; when MaxChildren is exceeded, a wildcard `<*>` branch is used

When a log line arrives:
1. Tokenize by whitespace
2. Navigate to the length node, then traverse token-by-token
3. If we reach an existing leaf cluster, update its template by generalizing any mismatched positions to `<*>`, then extract variables at wildcard positions
4. If we reach a new position (no leaf), create a new cluster with the exact tokens as the template

### Mapping Rules

Drain extraction only works when there's a hardcoded mapping rule from a template to graph elements. Currently there's one rule:

- **Template**: `"Connection to <*> failed"` → Creates an `Unknown` source node, an `Inferred` target node (ID = the captured variable), and a `FAILED_CONNECTION` edge

For any other template match, the pipeline returns an error instructing the agent to parse the text manually. This is by design — Drain is a proof-of-concept for log line parsing, not a general-purpose extraction engine.

### Limitations

Drain's simplified implementation has several limitations that make it unsuitable for structured data:
- **Tokenizes by whitespace**: `{"service":"api"}` becomes one token, losing all internal structure
- **No semantic understanding**: Can't distinguish field names from values
- **No mapping rules**: Even when a template matches, only positional variables are extracted — there's no way to interpret JSON semantics
- **Template generalization requires multiple passes**: Two different log lines must hit the same tree leaf for the template to develop wildcards

These limitations are precisely why the pipeline exists — structured data bypasses Drain entirely via format detection.

## Stage 4: Schema Scoring

**File**: `extract.go` (`MatchSchemasScored` function), `schema.go` (structural triple computation)

After extraction produces nodes and edges, the pipeline scores them against all registered schemas to identify which architectural patterns the data matches.

### The Scoring Formula

```
Score = 0.5 * EdgeCoverage + 0.3 * NodeCoverage + 0.2 * FieldConfidence
```

A schema matches if its score >= 0.6 (the threshold is lower than the previous 0.8 because the multi-dimensional formula compensates for individual dimension weaknesses).

### Dimension 1: Edge Coverage (weight 0.5)

Edge coverage measures structural match — what fraction of the input's relationship patterns exist in the schema.

**Algorithm**:
1. Compute the input's structural signature: for each edge, look up the source and target node types to form a `StructuralTriple` (e.g., `Service -> CALLS -> Service`)
2. Compute the schema's structural signature from its edge definitions (parsing "SourceType -> RELATION -> TargetType" strings)
3. Calculate: `|inputTriples ∩ schemaTriples| / |inputTriples|`

This is the most heavily weighted dimension because structural relationships are the strongest signal for architectural pattern matching. A dependency graph with `Service -> CALLS -> Service` and `Service -> CONNECTS_TO -> DataStoreInstance` edges strongly indicates the `http_k8s_datastore` or `http_vm_datastore` schema.

### Dimension 2: Node Coverage (weight 0.3)

Node coverage measures type presence — what fraction of the input's node types appear in the schema's allowed node list.

**Algorithm**:
1. Collect unique node types from the input (e.g., {Service, DataStoreInstance, HTTPEndpoint})
2. Check each against the schema's node list
3. Calculate: `|inputNodeTypes ∩ schemaNodeTypes| / |inputNodeTypes|`

This catches cases where edges don't perfectly match (perhaps due to naming differences in relationships) but the node types clearly belong to a particular schema.

### Dimension 3: Field Confidence (weight 0.2)

Field confidence measures naming similarity — how well do the input's node type names match the schema's, using token-based similarity?

**Algorithm**:
1. For each unique node type in the input, find the best TokenSimilarity score against all schema node types
2. Average these best scores across all input node types

This dimension handles the case where extractors create node types that don't exactly match schema names but are semantically close. For example, if an extractor created a "ServiceProcess" type and the schema has "Service", the token overlap ("service") would give a non-zero score.

### Why Three Dimensions?

A single intersection score (the previous approach) fails when:
- **Edge types match but node types have different names**: Edge coverage is high but the old single score would be dragged down by node-type naming mismatches
- **Node types match but no edges were extracted**: Service summary extraction produces Service nodes with no edges. The old approach would give 0% match since it only looked at edge triples
- **Approximate naming**: An extractor might create "HTTPEndpoint" while a schema lists "Endpoint". Token similarity catches this; exact string matching wouldn't

The weights (0.5, 0.3, 0.2) reflect that structural relationships are the strongest signal, type presence is second, and naming similarity is a tie-breaker.

## Field Matching: How Names Map to Types

**File**: `fieldmatch.go`

Across all extractors, field names from tool outputs need to be mapped to schema node types. The field matching system provides utilities for this mapping.

### Token-Based Name Decomposition

`TokenizeName` splits compound names into lowercase tokens by handling three naming conventions:

| Input | Convention | Tokens |
|-------|-----------|--------|
| `ServiceName` | PascalCase | `["service", "name"]` |
| `service_name` | snake_case | `["service", "name"]` |
| `net-peer-name` | kebab-case | `["net", "peer", "name"]` |
| `HTTPEndpoint` | Acronym + PascalCase | `["http", "endpoint"]` |
| `DataStoreInstance` | Multi-word PascalCase | `["data", "store", "instance"]` |

The algorithm:
1. Replace `_`, `-`, `.` with spaces
2. Walk runes, splitting on uppercase-to-lowercase transitions (PascalCase/camelCase boundaries)
3. Handle consecutive uppercase (acronyms like "HTTP") by splitting only when followed by lowercase

### Jaccard Similarity

`TokenSimilarity(a, b)` computes `|tokens(a) ∩ tokens(b)| / |tokens(a) ∪ tokens(b)|` on the tokenized forms.

Examples:
- `TokenSimilarity("service_name", "ServiceName") = 1.0` (identical token sets)
- `TokenSimilarity("Pod", "DataStoreInstance") = 0.0` (no token overlap)
- `TokenSimilarity("service", "service_name") = 0.5` (1 shared token / 2 union tokens)

### Alias Dictionary

For common mappings that token similarity can't handle (e.g., "db_system" → "DataStoreInstance"), a static alias dictionary provides exact lookup:

```go
var FieldAliases = map[string]string{
    "service_name": "Service",
    "db_system":    "DataStoreInstance",
    "endpoint":     "HTTPEndpoint",
    "pod":          "Pod",
    // ... 30+ entries
}
```

The `ResolveNodeType` function first checks aliases, then falls back to token similarity if no alias matches.

### Deterministic Node IDs

`MakeNodeID(nodeType, parts...)` generates consistent, type-prefixed identifiers:

```
MakeNodeID("Service", "frontend")           → "service:frontend"
MakeNodeID("DataStoreInstance", "mysql", "db-host") → "datastoreinstance:mysql:db-host"
```

This is critical for cross-tool merging. When the dependency graph extractor creates `service:payment-api` and the operations summary extractor also creates `service:payment-api`, the store's `ON CONFLICT DO UPDATE` merges them into a single node. Without deterministic IDs, the same service from two tools would create duplicate nodes.

## Schema Field Hints

**Files**: `models.go`, `schemas/*.yaml`

Schemas can declare field hints that help the extraction pipeline map tool output fields to node types:

```yaml
blueprint:
  field_hints:
    Service:
      aliases: ["svc", "service_name", "ServiceName", "service"]
    DataStoreInstance:
      aliases: ["db_system", "database", "db", "datastore"]
      id_fields: ["net_peer_name", "db_name", "host"]
    HTTPEndpoint:
      aliases: ["endpoint", "operation", "span_name", "operation_name"]
```

- **`aliases`**: Alternative names for this node type that might appear in tool outputs
- **`id_fields`**: Fields whose values should be included in the node ID for disambiguation (e.g., two DataStoreInstance nodes with different `net_peer_name` values should be separate nodes)

The `FieldHint` struct is:
```go
type FieldHint struct {
    Aliases  []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
    IDFields []string `json:"id_fields,omitempty" yaml:"id_fields,omitempty"`
}
```

Field hints are additive — schemas without hints continue to work. The global `FieldAliases` dictionary provides baseline mappings; per-schema hints allow schemas to customize the mapping for their specific context.

## Integration: How the Handler Uses the Pipeline

**File**: `tools.go` (`NewIngestHandler`)

The `ingest_knowledge` MCP tool handler integrates all stages:

```
1. If raw_text is non-empty:
   a. pipeline.Process(raw_text)
   b. If error → return ParsingError (agent should parse manually)
   c. If success → append extracted nodes/edges/stats to args

2. Load all schemas from store

3. MatchSchemasScored(all_nodes, all_edges, schemas)

4. Ingest nodes, edges, stats, events into store

5. Return success message with matched schema names and scores
```

The key design choice: extraction results are *appended* to any explicitly provided nodes/edges/stats. This means an agent can pass both `raw_text` (for automatic extraction) and explicit `nodes`/`edges` (for manual additions) in a single call.

## Error Handling Philosophy

The pipeline follows a "fail fast, explain clearly" approach:

| Scenario | Behavior | Rationale |
|----------|----------|-----------|
| JSON/YAML detected but no extractor matches | Return error: "structured data detected but no extractor matched" | Agent should parse the data itself |
| CSV detected | Return error: "CSV format detected but no extractor available" | No CSV extractors implemented yet |
| Plain text, no Drain template match | Return error: "no matching template found" | Agent should structure the data |
| Plain text, template matches but no mapping rule | Return error: "template matched but no mapping rule" | Agent should structure the data |
| Extractor matches but data is empty | Return result with low confidence (0.2-0.3) | Still ingests what it found; confidence signals quality |

Errors from the pipeline are returned as `mcp.CallToolResult{IsError: true}` with a message instructing the agent to parse the input and retry with structured `nodes`/`edges`. This preserves the original Drain-era behavior where unrecognized inputs get bounced back to the agent.

## Performance Characteristics

- **Format detection**: O(n) where n is input size — JSON/YAML parsing, CSV line counting
- **CanHandle checks**: O(1) per extractor — simple key presence tests on the parsed map
- **Extraction**: O(m) where m is the number of entries in the parsed data — linear scan creating nodes/edges
- **Schema scoring**: O(s * t) where s is schema count (4 builtin) and t is triple count — small constants
- **Total**: Dominated by the initial JSON/YAML parse, which is unavoidable

The pipeline does not use goroutines or parallelism. The input sizes are small enough (typical tool outputs are < 100KB) that serial processing adds negligible latency.
