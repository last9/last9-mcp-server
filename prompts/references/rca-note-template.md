# RCA Note Template

Use this template when recording an RCA via `add_knowledge_note`. The note title should follow the format: `RCA: <brief description> <date>`.

## Template

```markdown
## Impact Summary

- **Services affected**: [list service names]
- **Duration**: [start time] to [end time] ([total duration])
- **Severity**: [Critical/High/Medium/Low]
- **User impact**: [description of user-facing impact, e.g., "checkout failures for 12% of users"]
- **SLO/SLA impact**: [which SLOs were breached, by how much]

## Timeline

| Time | Event |
|------|-------|
| HH:MM | [First indicator: alert fired / metric crossed threshold] |
| HH:MM | [Key event: deployment, config change, traffic spike] |
| HH:MM | [Detection: how was the issue noticed] |
| HH:MM | [Mitigation started: what action was taken] |
| HH:MM | [Resolution: when the issue was fully resolved] |

## Root Cause

[1-3 sentences describing the root cause. Be specific about what failed and why.]

**Category**: [deployment / config change / resource exhaustion / dependency failure / traffic spike / bug / infrastructure / external]

## Contributing Factors

- [Factor 1: e.g., "No HPA configured, so the service could not scale to absorb the traffic increase"]
- [Factor 2: e.g., "Connection pool sized for 50 connections, but peak load required 120"]
- [Factor 3: e.g., "Missing circuit breaker on downstream call to payment-service"]

## Evidence Chain

1. [Evidence 1: metric/trace/log that supports the root cause, with tool used]
   - Source: `get_service_performance_details` — p95 latency increased from 45ms to 1200ms at 14:05
2. [Evidence 2]
   - Source: `get_change_events` — deployment of v2.3.1 at 14:02
3. [Evidence 3]
   - Source: `prometheus_range_query` — CPU throttling reached 80% after deploy
4. [Evidence 4]
   - Source: `get_exceptions` — ConnectionTimeoutException count jumped from 0 to 450/min

## Remediation

### Immediate (done)
- [What was done to resolve: rollback, scale up, config change, etc.]

### Short-term (next sprint)
- [ ] [Action item 1 with owner if known]
- [ ] [Action item 2]

### Long-term (backlog)
- [ ] [Preventive measure 1]
- [ ] [Preventive measure 2]

## Action Items

| Action | Owner | Priority | Status |
|--------|-------|----------|--------|
| [Action 1] | [team/person] | [P0/P1/P2] | [open/in-progress/done] |
| [Action 2] | [team/person] | [P0/P1/P2] | [open] |
```

## Usage Notes

- Link the note to all affected service nodes (e.g., `node_ids: ["svc:frontend", "svc:checkout"]`)
- Link to relevant edges if the RCA involves inter-service communication (e.g., `edge_refs: [{"source": "svc:frontend", "target": "svc:payment", "relation": "CALLS"}]`)
- Fill in what you know; leave sections with `[unknown]` if information is not available rather than omitting the section
- The evidence chain should trace the logical reasoning from symptom to root cause
- Action items should be concrete and actionable, not vague ("add monitoring" is vague; "add alert on p95 latency > 500ms for checkout-service" is concrete)
