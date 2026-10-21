# Testing Guide - provider-keycloak

## Overview

Provider-keycloak has comprehensive test coverage across multiple layers:
- **Unit tests**: Client library and helper functions
- **Integration tests**: Controller-level reconciliation with mocked Keycloak API
- **Coverage**: 80%+ of critical code paths

## Running Tests

### Run all tests
```bash
go test ./...
```

### Run with coverage
```bash
go test ./... -cover
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
```

### Run specific package
```bash
go test ./internal/clients/... -v
go test ./internal/controller/realm/... -v
```

### Run with filtering
```bash
go test -run TestObserve ./internal/controller/...
go test -run TestCreate ./internal/controller/...
```

## Test Structure

### Client Library Tests (`internal/clients/`)
Tests for the Keycloak HTTP client library:
- Token refresh and OAuth2 flow
- Request/response marshaling
- Error handling and validation
- TLS configuration

### Controller Tests (`internal/controller/*/`)
Each controller has tests for:
- **Observe**: Detecting resource state and drift
- **Create**: Creating new resources
- **Update**: Detecting and applying updates
- **Delete**: Removing resources

Example structure:
```go
func TestObserve(t *testing.T) {
    // Test: Resource exists and is up to date
    // Test: Resource exists but has drifted
    // Test: Resource does not exist
}

func TestCreate(t *testing.T) {
    // Test: Successful creation
    // Test: Creation failure
}

func TestUpdate(t *testing.T) {
    // Test: Successful update
    // Test: Update with no changes
}

func TestDelete(t *testing.T) {
    // Test: Successful deletion
    // Test: Deletion failure
}
```

## Test Data

All tests use realistic mock data matching Keycloak API responses:
- UUIDs for resource IDs
- Proper JSON structures
- Keycloak-compatible field names and types

## Mocking Strategy

Tests use Go's standard mock pattern:
```go
type mockClient struct {
    methodFn func(...) (type, error)
}

func (m *mockClient) Method(...) (type, error) {
    return m.methodFn(...)
}
```

Unmocked methods return zero values, allowing focused testing of specific behavior.

## Integration Testing

For testing against a real Keycloak instance:

1. Start Keycloak locally:
```bash
docker run -p 8080:8080 \
  -e KEYCLOAK_ADMIN=admin \
  -e KEYCLOAK_ADMIN_PASSWORD=admin \
  quay.io/keycloak/keycloak:latest \
  start-dev
```

2. Create test realm and client:
```bash
# Create realm via UI or API at http://localhost:8080/admin
```

3. Configure provider connection:
```yaml
apiVersion: keycloak.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: keycloak-local
spec:
  url: http://localhost:8080
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: keycloak-creds
      key: credentials
```

4. Create test resources:
```bash
kubectl apply -f config/samples/
```

## Test Coverage Goals

| Component | Target | Status |
|-----------|--------|--------|
| Client library | 85%+ | ✅ |
| Core controllers | 80%+ | ✅ |
| Authorization | 75%+ | ✅ |
| Error handling | 90%+ | ✅ |
| Overall | 80%+ | ✅ |

## Debugging Tests

### Verbose output
```bash
go test -v ./internal/controller/realm/...
```

### Run single test
```bash
go test -run TestObserve/resource_exists -v ./internal/controller/realm/...
```

### Debug with prints
```go
t.Logf("debugging info: %v", variable)
```

### Use -count to detect race conditions
```bash
go test -race -count=10 ./internal/controller/...
```

## CI/CD Integration

Tests run automatically on:
- Pull requests (all tests must pass)
- Commits to main branch
- Release builds (tests + coverage gates)

See `.github/workflows/` for CI configuration.

## Known Limitations

1. **Mock completeness**: Some mock implementations return nil/zero values. Enhance as needed for specific tests.
2. **Concurrency**: Keycloak API mocks are not concurrent-safe. Use sequential test execution.
3. **Long-running tests**: Authorization policy tests may take longer due to nested resource lookups.

## Contributing Tests

When adding new controllers:

1. Create `internal/controller/{resource}/{resource}_test.go`
2. Implement mockClient for Client interface
3. Write Observe/Create/Update/Delete tests
4. Ensure coverage > 80%
5. Update TESTING.md if adding new test patterns

## Performance Testing

To profile controller performance:

```bash
go test -cpuprofile=cpu.prof ./internal/controller/realm
go tool pprof cpu.prof
```

## Security Testing

Controller tests validate:
- No credentials logged
- Input validation on all user parameters
- TLS configuration enforced
- Secret references properly handled
