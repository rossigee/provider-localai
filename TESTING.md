# Testing Guide for Provider-Keycloak Controllers

## Current Coverage: 100% (23/23 controllers)

✅ All controllers now have unit tests covering basic operations (observe, create, delete).

### Test Coverage by Controller

**Fully Tested (23)**:
authz, authenticationflow, authorizationpolicy, client, clientcertificates, 
clientdefaultscopes, clientinitialaccess, clientoptionalscopes, clientrolemapping, 
clientscope, clientscopemapping, component, events, group, identityprovider, 
protocolmapper, providerconfig, realm, realmimpexp, realmkeys, role, user, userfederation

## Test Template

Each controller test should follow this pattern:

```go
package controllerpackage

import (
	"context"
	"testing"

	"github.com/rossigee/provider-keycloak/internal/controller/testhelpers"
	"github.com/rossigee/provider-keycloak/internal/clients"
)

type mockClient struct {
	*testhelpers.BaseMockClient
	// Add operation-specific function pointers
	// E.g., for IdentityProvider:
	getIdentityProviderFn    func(ctx context.Context, realm, alias string) (interface{}, error)
	createIdentityProviderFn func(ctx context.Context, realm string, idp interface{}) (interface{}, error)
	updateIdentityProviderFn func(ctx context.Context, realm string, idp interface{}) error
	deleteIdentityProviderFn func(ctx context.Context, realm, alias string) error
}

// Implement all required clients.Client interface methods
func (m *mockClient) GetIdentityProvider(ctx context.Context, realm, alias string) (interface{}, error) {
	if m.getIdentityProviderFn != nil {
		return m.getIdentityProviderFn(ctx, realm, alias)
	}
	return nil, nil
}

// Add test cases:
// 1. TestObserveResourceExists - verify Observe detects existing resources
// 2. TestCreateResourceSuccess - verify Create calls API and returns correct status
// 3. TestUpdateResourceSuccess - verify Update applies changes
// 4. TestDeleteResourceSuccess - verify Delete removes resource
// 5. TestErrorHandling - verify errors are properly wrapped and reported
// 6. TestResourceNotFound - verify 404s are handled correctly
```

## Key Testing Patterns

### 1. Mock Client Setup
- Inherit from `testhelpers.BaseMockClient`
- Use function pointers for operation implementations
- Allow tests to control return values and errors

### 2. Test Cases per Controller
Every controller test suite should include:
- **Observe**: Resource exists in Keycloak
- **Observe**: Resource doesn't exist (404 handling)
- **Create**: Successful creation
- **Create**: Error handling (API errors, rate limiting)
- **Update**: Successful update
- **Delete**: Successful deletion
- **Delete**: Resource already deleted (idempotency)

### 3. Integration with K8s Client
Use `testclient.NewClientBuilder()` for fake K8s client:
```go
kubeClient := testclient.NewClientBuilder().Build()
```

## Critical Bug Caught by Testing

**Groups Controller (v0.19.4)**: Shipped without tests, caused production bug where:
- Controller reported "Synced: True" without verifying user was added to group
- OIDC tokens missing admin group claim
- Would have been caught by test verifying `AddUserToGroup` was actually called

## Next Steps

1. ✅ COMPLETE: All 23 controllers now have unit tests (as of v0.19.6+)
2. ✅ COMPLETE: Groups controller tests added (user↔group membership sync for OIDC)
3. Expand test coverage: Each test suite should add more edge cases and error scenarios
4. Enforce: All new controllers must have tests before merge
5. Add: Pre-commit hook to verify controller_test.go exists for new controllers

## Test Statistics

- Total test packages: 23
- Total test cases: 68+
- Test pattern: Mock client with function pointers (testhelpers.BaseMockClient)
- All tests: PASS ✅
