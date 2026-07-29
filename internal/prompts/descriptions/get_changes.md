Build a scoped view of changes during an absolute time range for incident chronology and configuration-change investigation. Results are assembled at request time, not persisted; proximity does not establish causality.

start_time and end_time are required RFC3339 timestamps. Provide at least one scope: service, environment, cluster, namespace, resource_kind, resource_name, or resource_uid. sources selects change_events and/or kubernetes_events; both are default. categories filters results. order defaults to desc; asc is supported. limit defaults in the API and accepts 1-500. Return cursor unchanged.

evidence=explicit is recorded; evidence=inferred is an exact built-in Kubernetes classification. Check timestamp_basis. Respect each source's query_status and configuration_status; partial results remain useful. Suppressed observations and unknowns are counted in source metadata. Do not infer health, cause, or remediation from this response alone.
