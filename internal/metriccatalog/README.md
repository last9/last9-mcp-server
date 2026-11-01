# Receiver metric catalog

The lookup describes metric definitions, not proof that a receiver or metric is deployed.
It covers host CPU/disk/filesystem/load/memory/network/paging/process/processes,
kubelet, Kubernetes cluster, Docker, MongoDB, Oracle, Nginx, Kafka, SQL Server,
PostgreSQL, Redis, MySQL, Apache and IIS receivers from OTel contrib **v0.120.0**.

`receiver_sources.json` contains the name, instrument kind, monotonicity, unit and
help extracted from each upstream `metadata.yaml`, plus its path, version and
SHA-256. No tenant data is included. `receivers_generated.json` indexes those
records by sanitized names and Prometheus names with unit/type suffixes.
Non-monotonic sums become gauges. Units retain their OTel spelling (e.g. `By`,
`ms`): name translation does not rescale values. Conflicting aliases are omitted.
Custom exporter namespaces, renamed metrics and other receiver versions are not
assumed to match.

Regenerate offline from the committed inputs:

```sh
go run ./internal/metriccatalog/cmd/generate
go test ./internal/metriccatalog
```

Refresh inputs from their pinned upstream versions (Python 3 and PyYAML 6.0.1):

```sh
python3 scripts/refresh-receiver-metadata.py
```

Change the version in that script intentionally when upgrading, review the
resulting definitions and adjust the version pin in `TestReceiverLookupFresh`.
The freshness test regenerates and compares bytes without network or Python.

Upstream data and name translation are derived from the Apache-2.0 licensed
[OpenTelemetry Collector contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/v0.120.0/receiver)
and its [Prometheus translator](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.120.0/pkg/translator/prometheus/normalize_name.go).
Copyright The OpenTelemetry Authors; see `LICENSE.otel`.
