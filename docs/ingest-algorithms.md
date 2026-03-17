# Algorithms in `ingest_knowledge` Tool

The `ingest_knowledge` tool converts unstructured inputs (raw text from other tools) into structured knowledge graph entities using a multi-stage algorithmic pipeline. This document describes each algorithm in detail.

## Pipeline Overview

```
raw_text input
    │
    ▼
┌─────────────────────────┐
│  1. Format Detection    │  Heuristic detection: JSON > YAML > CSV > PlainText
└──────────┬──────────────┘
           │
           ├─ Structured (JSON/YAML)
           │      │
           │      ▼
           │  ┌─────────────────────────────────────────────┐
           │  │  2. Pattern Matching (ExtractorRegistry)    │
           │  │  Priority-ordered: first CanHandle() wins   │
           │  └──────────┬──────────────────────────────────┘
           │             │
           │             ├─ ComponentDiscoveryExtractor (discover_system_components)
           │             ├─ DependencyGraphExtractor (get_service_dependency_graph)
           │             ├─ OperationsSummaryExtractor (get_service_operations_summary)
           │             ├─ ServiceSummaryExtractor (get_service_summary)
           │             └─ PrometheusExtractor (prometheus queries)
           │
           └─ PlainText
                  │
                  ▼
              ┌─────────────────────────────────┐
              │  3. Drain Log Template Mining   │
              │  (simplified implementation)     │
              └──────────┬──────────────────────┘
                         │
                         ▼
                  Extraction Result
                         │
                         ▼
┌──────────────────────────────────────────────────────┐
│  4. Schema Matching (Multi-Dimensional Scoring)      │
│  Score = 0.5·Edge + 0.3·Node + 0.2·Field            │
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│  5. SQLite Storage with FTS5 Indexing                │
└──────────────────────────────────────────────────────┘
```

---

## Algorithm 1: Format Detection

**File**: `internal/knowledge/format.go`

**Purpose**: Identify the input format to route to the correct parser

### Detection Order (priority-based)

1. **JSON Detection**
   - Check if first non-whitespace char is `{` or `[`
   - Attempt `json.Unmarshal()`
   - If successful → `FormatJSON`

2. **YAML Detection**
   - Attempt `yaml.Unmarshal()` into `interface{}`
   - Check if result is `map[string]interface{}` or `[]interface{}`
   - Empty string/nil → not YAML
   - If successful → `FormatYAML`

3. **CSV Detection** (heuristic)
   - Check if first line contains commas
   - Require at least 3 lines
   - Count fields per line (split by comma)
   - If field count consistent across lines → `FormatCSV`

4. **PlainText** (fallback)
   - Default when no other format matches

### Key Insight

JSON prioritized over YAML because valid JSON is also valid YAML, but not vice versa. This prevents ambiguity.

---

## Algorithm 2: Pattern Matching via Extractors

**File**: `internal/knowledge/extract.go`

**Purpose**: Recognize tool output shapes and convert to graph entities

### ExtractorRegistry Algorithm

```
for each extractor in priority order:
    if extractor.CanHandle(parsed):
        result = extractor.Extract(parsed)
        result.Pattern = extractor.Name()
        return result
return nil  // No extractor matched
```

### Extractor Priority Order

1. **ComponentDiscoveryExtractor** — Most specific shape
   - `CanHandle`: requires `components` (map) + `triples` (array)
   - Creates nodes from component lists
   - Creates edges from triple relationships

2. **DependencyGraphExtractor**
   - `CanHandle`: requires `service_name` + at least one of (`incoming`, `outgoing`, `databases`, `messaging_systems`)
   - Creates Service nodes for incoming/outgoing
   - Creates DataStoreInstance for databases
   - Creates KafkaTopic for messaging
   - Extracts RED metrics (Throughput, ErrorRate, ResponseTime) as Statistics

3. **OperationsSummaryExtractor**
   - `CanHandle`: requires `service_name` (string) + `operations` (array)
   - Creates HTTPEndpoint nodes per operation
   - Creates sub-nodes for db_system (DataStoreInstance) and messaging_system (KafkaTopic)
   - EXPOSES edges from service to endpoints
   - CONNECTS_TO / PRODUCES_TO edges for databases/messaging

4. **ServiceSummaryExtractor**
   - `CanHandle`: top-level object where all values are objects with `ServiceName` or `Throughput`
   - Creates one Service node per top-level key
   - Extracts Throughput, ErrorRate, ResponseTime as Statistics

5. **PrometheusExtractor** — Most complex, see Algorithm 3
   - `CanHandle`: array where elements have `metric` (map) + (`value` OR `values`)
   - See detailed algorithm below

### Why Priority Ordering?

Each extractor's `CanHandle()` checks are fast (O(1) map lookups, O(n) array scans). Patterns are sufficiently distinct that false positives are rare. First match wins to avoid unnecessary processing.

---

## Algorithm 3: Prometheus Topology Extraction

**Files**: `internal/knowledge/extract_prometheus.go`, `internal/knowledge/prom_rules.go`

**Purpose**: Convert Prometheus metric labels into graph nodes and edges

### Phase 1: Entity Resolution

For each Prometheus series `{metric_labels}`:

1. **Resolve Entities** via declarative rules:
   ```
   for each entity_rule in defaultEntityRules:
       canonical_label = rule.Label  // e.g. "namespace"
       value = resolveLabel(canonical_label, metric_labels)  // tries aliases
       if value found AND metric_name matches rule.MetricPrefixes:
           build scoped_id from rule.ScopeLabels + value
           add resolvedEntity{label, nodeID, nodeType, name, priority}
   ```

2. **Label Alias Resolution** (`resolveLabel`):
   ```
   canonical → aliases lookup:
     "namespace" → ["namespace", "k8s_namespace_name"]
     "service" → ["service", "service_name"]
     "pod" → ["pod", "k8s_pod_name"]
     ...

   for each alias in order:
       if labels[alias] exists and non-empty:
           return value

   fallback to direct lookup: labels[canonical]
   ```

3. **Scoped Node ID Generation**:
   ```
   Container needs uniqueness within Pod:
     ScopeLabels = ["namespace", "pod"]
     nodeID = "container:default:nginx-abc:nginx"

   Pod needs uniqueness within Namespace:
     ScopeLabels = ["namespace"]
     nodeID = "pod:default:nginx-abc"
   ```

4. **Environment Resolution** (`resolveEnv`):
   ```
   priority_order = ["environment", "env", "cluster"]

   for each label_name in priority_order:
       if labels[label_name] exists and non-empty:
           return value

   return ""
   ```

### Phase 2: Node Creation with Env-Aware Dedup

```
nodeSet = {}

for each series in prometheus_result:
    entities = resolveEntities(series.labels)
    env = resolveEnv(series.labels)

    for each entity:
        if nodeSet[entity.nodeID] not exists:
            nodeSet[entity.nodeID] = Node{
                ID: entity.nodeID,
                Type: entity.nodeType,
                Name: entity.name,
                Env: env
            }
        else if nodeSet[entity.nodeID].Env == "" AND env != "":
            # Upgrade empty env to non-empty (first non-empty wins)
            nodeSet[entity.nodeID].Env = env
```

**Why Env-Aware Dedup?**

Same node can appear across multiple series. First series might lack `cluster` label but second series has it. This upgrades the node without overwriting if env already set.

### Phase 3: Edge Inference

```
byLabel = {}  # canonical_label → nodeID map
for each entity:
    byLabel[entity.label] = entity.nodeID

for each edge_rule in defaultEdgeRules:
    srcID = byLabel[rule.SourceLabel]
    tgtID = byLabel[rule.TargetLabel]
    if both exist:
        edges.add(Edge{srcID, tgtID, rule.Relation})
```

**Edge Rules** (label co-occurrence):

| Source Label | Target Label | Relation |
|-------------|-------------|----------|
| namespace | deployment | CONTAINS |
| namespace | pod | CONTAINS |
| pod | container | RUNS |
| pod | node | RUNS_ON |
| consumergroup | topic | CONSUMES_FROM |

### Phase 4: Stat Attachment & Qualification

1. **Pick Stat Target** (highest priority entity):
   ```
   priorities:
     Container = 4 (most specific)
     Pod = 3
     Deployment = 2
     Service = 2
     Namespace = 1
     VirtualMachine = 1 (least specific)

   target = entity with max(priority)
   fallback = resolvePrometheusNodeID(labels)  # service_name/job/instance
   ```

2. **Qualify Metric Name**:
   ```
   qualifiers = ["resource"]  # extendable

   base_name = "__name__"
   for each qualifier_label:
       if labels[qualifier_label] exists:
           base_name += ":" + labels[qualifier_label]

   unit = labels["unit"] if present

   Example: kube_pod_container_resource_requests + resource=cpu
            → "kube_pod_container_resource_requests:cpu"
   ```

### Entity Rules Table

| Canonical Label | Aliases | Node Type | Scope Labels | Priority | Constraints |
|----------------|---------|-----------|--------------|----------|-------------|
| namespace | namespace, k8s_namespace_name | Namespace | — | 1 | — |
| deployment | deployment, k8s_deployment_name | Deployment | namespace | 2 | — |
| service | service, service_name | Service | namespace | 2 | — |
| pod | pod, k8s_pod_name | Pod | namespace | 3 | — |
| container | container, k8s_container_name | Container | namespace, pod | 4 | — |
| node | node, k8s_node_name | VirtualMachine | — | 1 | — |
| instance | instance | VirtualMachine | — | 1 | node_* prefix only |
| topic | topic, redpanda_topic | KafkaTopic | — | 2 | — |
| consumergroup | consumergroup, redpanda_group | KafkaConsumerGroup | — | 2 | — |

**Why `instance` prefix constraint?**

The `instance` label appears on ALL Prometheus metrics (it's the scrape target). Creating a VirtualMachine node for every series would pollute the graph. Only `node_*` metrics (node exporter) genuinely identify a host.

---

## Algorithm 4: Field Matching (Token Similarity)

**File**: `internal/knowledge/fieldmatch.go`

**Purpose**: Match tool output field names to schema node types despite naming variations

### TokenizeName Algorithm

Splits compound names into lowercase tokens, handling multiple conventions:

```
Separators: _, -, .
CamelCase: serviceName → ["service", "name"]
PascalCase: ServiceName → ["service", "name"]
Acronyms: HTTPEndpoint → ["http", "endpoint"]

Algorithm:
1. Replace separators with spaces
2. Iterate runes:
   a. If space → flush current token
   b. If uppercase after lowercase → split
   c. If uppercase before lowercase (acronym boundary) → split
   d. Convert to lowercase and append to current token
3. Flush final token
```

**Examples**:
- `service_name` → `["service", "name"]`
- `ServiceName` → `["service", "name"]`
- `net-peer-name` → `["net", "peer", "name"]`
- `HTTPEndpoint` → `["http", "endpoint"]`
- `db_system` → `["db", "system"]`

### Jaccard Similarity Algorithm

```
TokenSimilarity(a, b):
    tokensA = TokenizeName(a)
    tokensB = TokenizeName(b)

    if both empty: return 1.0
    if either empty: return 0.0

    setA = set(tokensA)
    setB = set(tokensB)

    intersection = |setA ∩ setB|
    union = |setA ∪ setB|

    return intersection / union
```

**Why Jaccard?**

Captures semantic similarity between compound words that edit distance would miss:
- `TokenSimilarity("service_name", "ServiceName")` = 1.0 (edit distance = 1)
- `TokenSimilarity("db_system", "DataStore")` = 0.25 (edit distance = 8)

### ResolveNodeType Algorithm

```
ResolveNodeType(fieldName, schemaNodeTypes):
    # 1. Check static alias dictionary
    if FieldAliases[fieldName] exists:
        return FieldAliases[fieldName]

    # 2. Token similarity against schema nodes
    bestScore = 0.0
    bestType = ""
    for each nodeType in schemaNodeTypes:
        score = TokenSimilarity(fieldName, nodeType)
        if score > bestScore:
            bestScore = score
            bestType = nodeType

    if bestScore >= 0.5:
        return bestType

    return ""  # No match
```

**FieldAliases** (static dictionary):
```go
{
    "service_name": "Service",
    "db_system": "DataStoreInstance",
    "endpoint": "HTTPEndpoint",
    "pod": "Pod",
    "container": "Container",
    ...
}
```

---

## Algorithm 5: Schema Matching (Multi-Dimensional Scoring)

**File**: `internal/knowledge/extract.go` — `MatchSchemasScored()`

**Purpose**: Score extracted graph against registered schemas to identify architectural pattern

### Scoring Formula

```
Score = 0.5 × EdgeCoverage + 0.3 × NodeCoverage + 0.2 × FieldConfidence

where:
  EdgeCoverage = |input_triples ∩ schema_triples| / |input_triples|
  NodeCoverage = |input_node_types ∩ schema_node_types| / |input_node_types|
  FieldConfidence = avg(max(TokenSimilarity(input_type, schema_type)))

Threshold: Score ≥ 0.6 for match
```

### Algorithm Steps

1. **Compute Input Signature**:
   ```
   inputSig = set of edge triples "SourceType -> RELATION -> TargetType"
   inputNodeTypes = set of unique node types from input
   ```

2. **For Each Schema**:

   a. **Edge Coverage**:
   ```
   schemaSig = parse schema.Blueprint.Edges into triples
   intersection = count(inputSig ∩ schemaSig)
   edgeCoverage = intersection / |inputSig|
   ```

   b. **Node Coverage**:
   ```
   schemaNodeSet = set(schema.Blueprint.Nodes)
   matchedNodes = count(inputNodeTypes ∩ schemaNodeSet)
   nodeCoverage = matchedNodes / |inputNodeTypes|
   ```

   c. **Field Confidence**:
   ```
   for each inputNodeType:
       bestSim = max(TokenSimilarity(inputNodeType, schemaNodeType)
                     for schemaNodeType in schema.Blueprint.Nodes)
       totalSim += bestSim

   fieldConfidence = totalSim / |inputNodeTypes|
   ```

3. **Weighted Score**:
   ```
   score = 0.5*edgeCoverage + 0.3*nodeCoverage + 0.2*fieldConfidence

   if score >= 0.6:
       matches.append(schema)
   ```

4. **Sort by Score** (descending)

### Weight Rationale

- **Edge Coverage (50%)**: Most important — structural match of relationships
- **Node Coverage (30%)**: Type presence matters but naming varies
- **Field Confidence (20%)**: Fills gap when node type names differ (e.g. `Service` vs `ServiceInstance`)

**Example**:

Input: `Service`, `HTTPEndpoint`, `DataStoreInstance` with edges `Service->EXPOSES->HTTPEndpoint`, `Service->CALLS->DataStoreInstance`

Schema: `http_k8s_datastore` with nodes `Service`, `HTTPEndpoint`, `DataStoreInstance`, `Pod`, `Container` and matching edge patterns

```
EdgeCoverage = 2/2 = 1.0
NodeCoverage = 3/3 = 1.0
FieldConfidence = (1.0 + 1.0 + 1.0) / 3 = 1.0
Score = 0.5*1.0 + 0.3*1.0 + 0.2*1.0 = 1.0 ✓ (perfect match)
```

---

## Algorithm 6: Drain Log Template Mining

**File**: `internal/knowledge/drain.go`

**Purpose**: Extract templates from plain text log lines (simplified implementation)

### Drain Algorithm (Simplified)

Based on the [Drain paper](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) but simplified.

1. **Tokenization**:
   ```
   tokens = strings.Fields(logLine)  // whitespace split
   ```

2. **Tree Navigation** (depth-based routing):

   Level 1 — **Length Layer**:
   ```
   lengthToken = string(len(tokens))
   node = root.Children[lengthToken]
   ```

   Level 2..N — **Token Layers**:
   ```
   for i in 0..depth:
       token = tokens[i]
       if node.Children[token] exists:
           node = node.Children[token]  // exact match
       else if len(node.Children) < MaxChildren:
           node.Children[token] = newNode  // create branch
           node = newNode
       else:
           node = node.Children["<*>"]  // wildcard fallback
   ```

3. **Leaf Node** — Template Matching:
   ```
   if node.IsLeaf:
       cluster = Clusters[node.ClusterID]
       return cluster.ID, extractVariables(tokens, cluster.Template)
   else:
       clusterID = generateID()
       cluster = createCluster(tokens as template)
       Clusters[clusterID] = cluster
       node.IsLeaf = true
       node.ClusterID = clusterID
       return clusterID, []
   ```

4. **Variable Extraction**:
   ```
   Match template tokens against log tokens:
     - "<*>" → capture as variable
     - literal → skip

   Return captured variables
   ```

### Hardcoded Mapping Rule

```
if template matches "Connection to <*> failed":
    target = variables[0]
    return ExtractionResult{
        Nodes: [
            {ID: "unknown:source", Type: "Unknown"},
            {ID: target, Type: "Inferred"}
        ],
        Edges: [
            {Source: "unknown:source", Target: target, Relation: "FAILED_CONNECTION"}
        ]
    }
```

**Why Hardcoded?**

The simplified Drain implementation doesn't generalize templates well. The hardcoded rule demonstrates the concept but isn't production-ready. Full Drain would learn templates dynamically.

---

## Algorithm 7: Deterministic Node ID Generation

**File**: `internal/knowledge/fieldmatch.go` — `MakeNodeID()`

**Purpose**: Ensure the same entity gets the same ID across different tools for deduplication

### Algorithm

```
MakeNodeID(nodeType, parts...):
    prefix = lowercase(nodeType)

    if no parts:
        return prefix

    cleaned = filter(parts, non-empty)

    if no cleaned parts:
        return prefix

    return prefix + ":" + join(cleaned, ":")
```

**Examples**:
- `MakeNodeID("Service", "frontend")` → `"service:frontend"`
- `MakeNodeID("Pod", "default", "nginx-abc")` → `"pod:default:nginx-abc"`
- `MakeNodeID("DataStoreInstance", "mysql", "db-host")` → `"datastoreinstance:mysql:db-host"`

**Why Deterministic?**

Cross-tool merging requires the same entity to generate the same ID:
- Prometheus series creates `pod:default:nginx-abc`
- `discover_system_components` also creates `pod:default:nginx-abc`
- SQLite UPSERT merges them into one node

---

## Storage Algorithm: SQLite with FTS5

**File**: `internal/knowledge/store.go`

### Node Upsert with COALESCE

```sql
INSERT INTO nodes (id, type, name, env, properties, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    type = excluded.type,
    name = excluded.name,
    env = COALESCE(excluded.env, nodes.env),  -- preserve existing env if new is empty
    properties = json_patch(nodes.properties, excluded.properties),
    updated_at = excluded.updated_at
```

**Why COALESCE?**

Extractors that don't have env data (ComponentDiscovery, Prometheus without cluster label) pass `NULL`. COALESCE prevents overwriting existing env when upserting.

### FTS5 External Content

```sql
CREATE VIRTUAL TABLE nodes_fts USING fts5(
    id UNINDEXED,
    type,
    name,
    env,
    content=nodes,        -- external content reference
    content_rowid=rowid   -- join key
);
```

**Triggers** maintain sync:
```sql
-- AFTER INSERT: add to FTS
INSERT INTO nodes_fts(rowid, id, type, name, env)
VALUES (new.rowid, new.id, new.type, new.name, new.env);

-- AFTER UPDATE: delete old, insert new
DELETE FROM nodes_fts WHERE rowid = old.rowid;
INSERT INTO nodes_fts(rowid, id, type, name, env)
VALUES (new.rowid, new.id, new.type, new.name, new.env);

-- AFTER DELETE: remove from FTS
DELETE FROM nodes_fts WHERE rowid = old.rowid;
```

**Search Query** (JOIN required for external content):
```sql
SELECT n.*
FROM nodes_fts fts
JOIN nodes n ON n.rowid = fts.rowid
WHERE nodes_fts MATCH ?
LIMIT ?
```

---

## Performance Characteristics

| Algorithm | Complexity | Notes |
|-----------|-----------|-------|
| Format Detection | O(n) | Single pass through input string |
| Extractor Dispatch | O(k) | k = number of extractors (constant 5) |
| Prometheus Entity Resolution | O(m·r) | m = series count, r = entity rules (constant 9) |
| Token Similarity | O(a+b) | a, b = token counts (typically < 10) |
| Schema Matching | O(s·(n+e)) | s = schemas, n = nodes, e = edges |
| Drain Tree Navigation | O(d) | d = depth (constant 4) |
| Node Upsert | O(log n) | SQLite B-tree |
| FTS5 Search | O(log n) | Full-text index |

All algorithms scale linearly or better with input size. The most expensive operation is schema matching when many schemas exist, but remains practical (4 builtin schemas, typically < 10 custom).

---

## Trade-Offs & Design Decisions

### 1. Priority-Ordered Extractors vs Scoring All

**Decision**: First match wins

**Rationale**: Patterns are sufficiently distinct that false positives are rare. Priority ordering is faster (avg case: 1-2 CanHandle checks) and simpler than scoring all extractors.

### 2. Jaccard Similarity vs Edit Distance

**Decision**: Jaccard on tokenized names

**Rationale**: Compound words (`service_name` vs `ServiceName`) have identical semantic meaning but high edit distance. Token overlap captures this.

### 3. Separate resolveEnv vs Adding to labelAliases

**Decision**: Separate function with explicit priority order

**Rationale**: `cluster` is metadata that enriches nodes but doesn't create entities. Adding it to `labelAliases` would create spurious "Cluster" nodes.

### 4. Env-Aware Dedup vs Always Overwrite

**Decision**: First non-empty wins, upgrade from empty

**Rationale**: Same node appears across multiple Prometheus series. First series may lack `cluster` but second has it. Upgrading is correct; overwriting risks downgrading specific→generic.

### 5. Drain vs Full NLP Parsing

**Decision**: Simplified Drain with hardcoded rules

**Rationale**: Plain text is a fallback for unstructured logs. Most tool outputs are JSON/YAML where extractors work well. Full Drain would add complexity for minimal gain.

### 6. Multi-Dimensional Schema Scoring vs Simple Intersection

**Decision**: Weighted formula (0.5·Edge + 0.3·Node + 0.2·Field)

**Rationale**: Node type names vary (`Service` vs `ServiceInstance`). Field confidence via token similarity fills this gap. Edge coverage remains primary signal (structural match).
