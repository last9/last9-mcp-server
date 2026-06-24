
Fetches the distinct values for a single trace attribute/tag.

Use this after get_trace_attributes to see what values exist for a given field —
for example, all deployment environments, team names, or HTTP methods.

Accepts the tag name in any of these forms:
  - raw API name:    resource_department, event_exception.type
  - filter syntax:  resources['department'], events['exception.type'], or attributes['http.method']

Returns the canonical filter_field ready to use in a get_traces tracejson query,
plus an example condition.

Optionally pass a pipeline to scope the returned values to a filtered slice of
spans (same pipeline shape as get_traces). Omit it for global values.
