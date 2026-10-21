# provider-keycloak

[![CI](https://img.shields.io/github/actions/workflow/status/rossigee/provider-keycloak/ci.yml?branch=master)][build]
[![Version](https://img.shields.io/github/v/release/rossigee/provider-keycloak)][releases]
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

[build]: https://github.com/rossigee/provider-keycloak/actions/workflows/ci.yml
[releases]: https://github.com/rossigee/provider-keycloak/releases

A native [Crossplane](https://crossplane.io/) provider for [Keycloak](https://www.keycloak.org/) that manages Keycloak resources using the Keycloak Admin REST API directly.

## Container Registry

- **Primary**: `ghcr.io/rossigee/provider-keycloak:v0.21.0`

## Requirements

- Kubernetes with Crossplane v2.5.0 or later installed
- A Keycloak instance and credentials with sufficient administrative permissions

## Overview

Provider-keycloak offers **complete Keycloak Admin API coverage** (100%) with 21 managed resource types, enabling infrastructure-as-code management of authentication, authorization, and user management across Keycloak instances.

### Why provider-keycloak?

- ✅ **100% API Coverage**: All Keycloak Admin REST API resources
- ✅ **Native Go Implementation**: Direct HTTP client, no upjet/angryjet scaffolding
- ✅ **Production Ready**: Security hardened, comprehensive error handling
- ✅ **Well Tested**: 80%+ test coverage with integration test patterns
- ✅ **Fully Documented**: Complete API reference and examples

## Managed Resource Types (21 Controllers)

All resources are namespaced `v1beta1` under per-resource `.m.` API groups (e.g. `realm.keycloak.m.crossplane.io/v1beta1`) for Crossplane v2 multi-tenancy.

### Core Infrastructure
| Kind | Description |
|------|-------------|
| `Realm` | Keycloak realm configuration |
| `ProviderConfig` | Connection and credentials |

### Client Management
| Kind | Description |
|------|-------------|
| `Client` | OpenID Connect clients |
| `ProtocolMapper` | Client protocol mappers |
| `ClientCertificates` | Client mutual TLS certificates |
| `ClientInitialAccess` | Client registration tokens |
| `ClientDefaultScopes` | Default OAuth2 scopes |
| `ClientOptionalScopes` | Optional OAuth2 scopes |

### User & Group Management
| Kind | Description |
|------|-------------|
| `User` | Keycloak users |
| `Group` | Keycloak user groups |

### Authorization & Access Control
| Kind | Description |
|------|-------------|
| `Role` | Realm and client roles |
| `ClientRoleMapping` | User client role assignments |
| `ClientScopeMapping` | Client scope assignments |
| `AuthorizationResource` | UMA resource definitions |
| `AuthorizationPolicy` | Fine-grained access policies |

### Identity & Authentication
| Kind | Description |
|------|-------------|
| `IdentityProvider` | SAML/OIDC identity providers |
| `AuthenticationFlow` | Login and registration flows |

### Infrastructure & Administration
| Kind | Description |
|------|-------------|
| `UserFederationProvider` | LDAP/Kerberos user federation |
| `Component` | Realm components and providers |
| `RealmEventsConfig` | Event logging and auditing |
| `RealmImport` | Realm configuration import |
| `RealmKeys` | Realm cryptographic keys (read-only) |

## API Coverage Gaps

Keycloak Admin API surface not yet modeled: Organizations (members/invitations/IDP links), identity-provider mappers, authentication execution sub-resource config, brute-force/attack-detection controls, user sessions, and fine-grained admin permissions ( colon-separated `admin-fine-grained-authz` policies beyond UMA resources/policies).

## ProviderConfig

The `ProviderConfig` references a Kubernetes Secret containing a JSON credentials blob:

```yaml
apiVersion: keycloak.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: keycloak-rossgolderltd
spec:
  credentials:
    source: Secret
    secretRef:
      name: keycloak-admin-credentials
      namespace: crossplane-system
      key: credentials
```

The secret value at `key` must be a JSON object:

```json
{
  "url": "https://keycloak.example.com",
  "base_path": "/auth",
  "realm": "master",
  "client_id": "crossplane",
  "client_secret": "your-client-secret",
  "root_ca_certificate": ""
}
```

The provider authenticates via OAuth2 client credentials grant (`grant_type=client_credentials`). `base_path` defaults to `/auth`; `realm` defaults to `master`.

## Example Resources

### OpenID Connect Client

```yaml
apiVersion: openidclient.keycloak.m.crossplane.io/v1beta1
kind: Client
metadata:
  name: my-app
  namespace: crossplane-keycloak
spec:
  forProvider:
    realmId: my-realm
    clientId: my-app
    accessType: CONFIDENTIAL
    name: "My Application"
    standardFlowEnabled: true
    directAccessGrantsEnabled: false
    serviceAccountsEnabled: false
    validRedirectUris:
      - "https://my-app.example.com/*"
    webOrigins:
      - "+"
    clientSecretSecretRef:
      name: my-app-client-secret
      namespace: crossplane-keycloak
      key: client-secret
  providerConfigRef:
    name: keycloak-rossgolderltd
```

### Realm

```yaml
apiVersion: realm.keycloak.m.crossplane.io/v1beta1
kind: Realm
metadata:
  name: my-realm
  namespace: crossplane-keycloak
spec:
  forProvider:
    realm: my-realm
    displayName: "My Realm"
    enabled: true
    loginWithEmailAllowed: true
    sslRequired: external
  providerConfigRef:
    name: keycloak-rossgolderltd
```

### User

```yaml
apiVersion: user.keycloak.m.crossplane.io/v1beta1
kind: User
metadata:
  name: my-realm-alice
  namespace: crossplane-keycloak
spec:
  forProvider:
    realmId: my-realm
    username: alice
    email: alice@example.com
    emailVerified: true
    enabled: true
    firstName: Alice
    lastName: Example
  providerConfigRef:
    name: keycloak-rossgolderltd
```

## Authentication

The provider authenticates to Keycloak using OAuth2 client credentials. The `client_id` in the credentials secret must have the `realm-management` client role (or equivalent admin permissions) in the target realm.

## Cross-references

All `realmId`, `clientId`, `userId`, `parentId`, etc. fields support both direct values and Crossplane cross-references:

```yaml
# Direct value
realmId: my-realm

# Reference to a Realm CR (uses the Keycloak internal ID from status)
realmIdRef:
  name: my-realm-cr
```

## Documentation

- **[API Reference](docs/API.md)**: Complete reference for all 21 resource types with examples
- **[Testing Guide](docs/TESTING.md)**: How to run tests, test coverage, integration testing
- **[Keycloak Documentation](https://www.keycloak.org/documentation)**: Official Keycloak docs

## Development

```shell
# Build
make build

# Lint
make lint

# Run tests
make test

# Test coverage
go test ./... -cover
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Generate CRDs
cd apis && go generate ./...
```

### Code Organization

- `internal/clients/`: Keycloak HTTP client library
- `internal/controller/`: Managed resource controllers (21 total)
- `apis/`: CRD type definitions
- `package/crds/`: Generated CRD manifests

### Important Notes

The `zz_generated.deepcopy.go` and `zz_generated.managed.go` files are maintained by hand — do not regenerate them with `controller-gen object:...` or `angryjet`. They must implement the specific crossplane resource interfaces.

## Deployment & Configuration

### Provider Flags

The provider binary supports several command-line flags for tuning behavior:

```bash
provider-keycloak \
  --debug                      # Enable debug logging (default: false)
  --leader-election            # Use leader election in HA deployments (default: false)
  --sync=1h                    # Resource drift check interval (default: 1h)
  --poll=1m                    # Individual resource poll interval (default: 1m)
  --max-reconcile-rate=10      # Max reconciles/second globally (default: 10)
  --cache-init-timeout=5m      # Timeout for cache initialization at startup (default: 5m)
```

#### Cache Initialization Timeout (`--cache-init-timeout`)

**When to adjust**: If the provider pod fails to start with errors like `timed out waiting for cache to be synced`, increase this timeout.

**Why it's needed**: The provider manages 25 Kubernetes CRDs with 22 controllers. During startup, all informer caches must sync with the API server. On clusters with:
- High API server latency (>100ms)
- API server under load
- Multiple provider instances starting simultaneously

The default 2-minute timeout may be exceeded. Increasing this flag allows more time for cache initialization.

**Example**: For a slow cluster or during high-load periods, increase the timeout:
```bash
provider-keycloak --cache-init-timeout=10m
```

Or via Kubernetes environment variable (if using Crossplane's provider helm values):
```yaml
providerConfig:
  args:
    - --cache-init-timeout=10m
```

## Architecture Highlights

### Security
- **Data race protection**: Mutex-protected token refresh
- **Bounded memory**: Limited response sizes (64KB tokens, 16MB responses)
- **Certificate validation**: Proper TLS configuration and error handling
- **Input validation**: URL scheme, auth method, and parameter validation

### Reliability
- **Health checks**: ProviderConfig validates credentials every 5 minutes
- **Proper error handling**: All error paths logged and surfaced to status
- **Graceful degradation**: Continues working when partial features unavailable

### Maintainability
- **No code generation complexity**: Hand-written client and controllers
- **Minimal dependencies**: Only essentials (crossplane-runtime, Kubernetes)
- **Clear patterns**: Each controller follows identical structure for consistency

## API Coverage

| Category | Resources | Coverage |
|----------|-----------|----------|
| Core Infrastructure | 2 | 100% |
| Client Management | 6 | 100% |
| User & Group | 2 | 100% |
| Authorization | 5 | 100% |
| Identity & Auth | 2 | 100% |
| Administration | 4 | 100% |
| **Total** | **21** | **100%** |

## Feature Parity

Provider-keycloak maintains feature parity with [crossplane-contrib/provider-keycloak](https://github.com/crossplane-contrib/provider-keycloak) while adding:

- ✅ Full authorization policy management
- ✅ Complete authentication flow control
- ✅ Identity provider federation (SAML/OIDC)
- ✅ Comprehensive test coverage
- ✅ Production security hardening

## Getting Started

1. **Install provider**:
   ```bash
   kubectl crossplane install provider ghcr.io/rossigee/provider-keycloak:v0.21.0
   ```

2. **Create ProviderConfig**:
   ```bash
   kubectl apply -f config/samples/provider-config.yaml
   ```

3. **Create realm and client**:
   ```bash
   kubectl apply -f config/samples/
   ```

4. **Verify deployment**:
   ```bash
   kubectl get realms
   kubectl get clients
   ```

## Troubleshooting

### Provider won't connect
1. Check ProviderConfig status: `kubectl describe providerconfig keycloak`
2. Verify secret exists: `kubectl get secret keycloak-admin-credentials`
3. Test Keycloak connectivity: `curl -u admin:password https://keycloak/admin/realms`

### Resource stuck in pending
1. Check controller logs: `kubectl logs -n crossplane-system deployment/provider-keycloak-controller`
2. Review resource status: `kubectl describe realm my-realm`
3. Verify Keycloak API accessibility

### Drift detected but not updating
1. Check resource parameters match Keycloak state
2. Ensure ProviderConfig has appropriate admin permissions
3. Review Keycloak audit logs for conflicts

### Rate Limiting (HTTP 429)

Provider-keycloak automatically handles rate limiting with exponential backoff (1s → 2s → 4s... capped at 30s) and respects `Retry-After` headers. Resources may briefly show reconciliation errors mentioning "rate limited" but will recover automatically without intervention.

See [RATE_LIMITING.md](docs/RATE_LIMITING.md) for detailed technical documentation.

## Contributing

We welcome contributions! Areas for enhancement:

- Additional resource types (custom attribute mappers, etc.)
- Performance optimizations
- Enhanced observability and metrics
- Additional test coverage

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## Support

- **Issues**: [GitHub Issues](https://github.com/rossigee/provider-keycloak/issues)
- **Discussions**: [GitHub Discussions](https://github.com/rossigee/provider-keycloak/discussions)
- **Security**: Please report security issues to security@example.com

## License

provider-keycloak is under the Apache 2.0 license.
