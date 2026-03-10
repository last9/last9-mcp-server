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
