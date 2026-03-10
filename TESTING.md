# Testing

## Running Tests

```bash
go test ./...              # Run all tests
go test -v ./...           # Verbose output
go test ./internal/utils/...  # Specific package
go test -v -run TestName ./...  # Specific test
```

## Integration Tests

Integration tests require `TEST_REFRESH_TOKEN` (skipped if not set):

```bash
export TEST_REFRESH_TOKEN="your_refresh_token"
go test -v ./...
```

Get token from [Last9 API Access Settings](https://app.last9.io/settings/api-access).

**Note:** Never commit `TEST_REFRESH_TOKEN` to version control.

## Log Index Feature Tests

The log index parameter feature has comprehensive test coverage:

### Unit Tests

```bash
# Test index normalization and validation
go test -v -run TestNormalizeLogIndex ./internal/utils/...

# Test index resolution to dashboard parameters
go test -v -run TestResolveLogIndexDashboardParam ./internal/utils/...

# Test schema includes index parameter
go test -v -run TestLogToolsSchemaIncludesIndex ./internal/telemetry/logs/...
```

### Integration Tests

```bash
# Test complete index support flow including resolution, fallback, and deep links
go test -v -run TestIndexSupport ./internal/telemetry/logs/...
```

These tests verify:
- Index format validation (`physical_index:<name>`, `rehydration_index:<block_name>`)
- Index normalization and error handling
- Index resolution to dashboard IDs for deep links
- Fallback behavior when index is omitted
- Deep link metadata generation based on index resolution
- API parameter forwarding