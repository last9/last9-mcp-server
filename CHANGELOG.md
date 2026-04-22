# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.6.0] - 2026-04-19

### Added
- Per-query datasource selection for Prometheus tools (#129).

### Fixed
- Exception investigation now calls `get_service_traces` instead of `get_traces` (#122).

### Changed
- Rewrote README to cover all current tools with a cleaner structure (#128).

## [0.5.1] - 2026-03-02

### Fixed
- Use `[]map[string]interface{}` for `logjson_query` and `tracejson_query` schema (#93).

## [0.5.0] - 2026-02-28

### Added
- Max response time metric support in APM tools (#76).
- Increased max lookback window from 24h to 14 days.

### Fixed
- Correct curl testing examples to use MCP session handshake.
- Docs for hosted MCP, token type, Windows binary; telemetry disabled by default (#86).
- Note Claude Desktop does not support hosted HTTP MCP yet; revert to STDIO.

### Changed
- Bumped `go.opentelemetry.io/otel/sdk` to 1.40.0 (#91).
- Bumped `github.com/modelcontextprotocol/go-sdk` (#90).
- Updated README for v0.5.0 release (#92).

## [0.4.0] - 2026-02-17

### Added
- Deep link generation across handlers (dashboards, exceptions, service logs, AI assistant tools).
- Cluster parameter in dashboard deep links.

### Fixed
- Broken MCP tools (#73).
- Exception filter simplification and nested response handling.
- Exception attribute detection in traces.
- URL parameter escaping in deep link generation.
- Reference URLs for MCP tools.
- Match env label with regex in PromQL queries.
- Clarified `lookback_minutes` and time parameter defaults in docs (#74).

### Changed
- Improved tool descriptions (#78).
- Reverted "disable mutating tools by default until RBAC" — re-enabled.

## [0.3.0] - 2026-01-12

### Added
- Trace tools (#60).
- Refresh token support (#61).

### Changed
- Simplified authentication and cleanup (#64).
- Removed `and` condition from `get_trace_attributes`.

## [0.2.0] - 2025-11-03

### Added
- `get_traces` tool with trace ID and service name support (#58).
- `service_name` and `deployment_environment` filters in exceptions tool (#57).
- Docker image build for release branches (#54).
- Migration to official MCP SDK with telemetry (#46).

### Fixed
- Empty response from queries (#53, #55).

### Changed
- Tool improvements (#51).

[0.6.0]: https://github.com/last9/last9-mcp-server/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/last9/last9-mcp-server/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/last9/last9-mcp-server/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/last9/last9-mcp-server/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/last9/last9-mcp-server/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/last9/last9-mcp-server/compare/v0.1.15...v0.2.0
