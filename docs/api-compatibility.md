# API Compatibility Matrix

This document maps provider-keycloak resources to crossplane-contrib/provider-keycloak for migration compatibility.

## Overview

provider-keycloak maintains API compatibility with [crossplane-contrib/provider-keycloak](https://github.com/crossplane-contrib/provider-keycloak) v2.19.0, allowing existing manifests to work without modification.

**Key differences:**
- provider-keycloak uses direct Keycloak Admin REST API (no Terraform state overhead)
- All CRD names and API groups match crossplane-contrib/provider-keycloak
- Additional fields are additive - existing specs continue to work

---

## Client (openidclient.keycloak.m.crossplane.io/v1beta1)

### Field Support Matrix

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| **Basic Configuration** | | | |
| clientId | ✅ | ✅ | Required, OAuth2 client identifier |
| name | ✅ | ✅ | Display name |
| description | ✅ | ✅ | Client description |
| enabled | ✅ | ✅ | Enable/disable client |
| accessType | ✅ | ✅ | CONFIDENTIAL, PUBLIC, BEARER_ONLY |
| **URLs** | | | |
| rootUrl | ✅ | ✅ | Root URL for relative URLs |
| baseUrl | ✅ | ✅ | Default redirect base URL |
| homeUrl | ✅ | ⚠️ | **NEW** - Home URL for account console |
| adminUrl | ✅ | ⚠️ | **NEW** - Admin interface URL |
| frontchannelLogoutUrl | ✅ | ⚠️ | **NEW** - Front-channel logout URL |
| backchannelLogoutUrl | ✅ | ⚠️ | **NEW** - Back-channel logout URL |
| **Redirect/CORS** | | | |
| validRedirectUris | ✅ | ✅ | Allowed redirect URIs |
| validPostLogoutRedirectUris | ✅ | ⚠️ | **NEW** - Post-logout redirect URIs |
| webOrigins | ✅ | ✅ | CORS allowed origins |
| **Flow Configuration** | | | |
| standardFlowEnabled | ✅ | ✅ | OpenID Connect authorization code flow |
| implicitFlowEnabled | ✅ | ✅ | Implicit flow |
| directAccessGrantsEnabled | ✅ | ✅ | Resource owner password credentials |
| serviceAccountsEnabled | ✅ | ✅ | Client credentials (service accounts) |
| **Advanced Flags** | | | |
| publicClient | ✅ | ⚠️ | **NEW** - No client secret required |
| bearerOnly | ✅ | ⚠️ | **NEW** - Bearer token only (API) |
| consentRequired | ✅ | ⚠️ | **NEW** - User consent required |
| fullScopeAllowed | ✅ | ⚠️ | **NEW** - All roles mapped as scope |
| alwaysDisplayInConsole | ✅ | ⚠️ | **NEW** - Always show in account console |
| **Logout Configuration** | | | |
| frontchannelLogoutEnabled | ✅ | ⚠️ | **NEW** - Enable front-channel logout |
| backchannelLogoutSessionRequired | ✅ | ⚠️ | **NEW** - Session required for logout |
| backchannelLogoutRevokeOfflineSessions | ✅ | ⚠️ | **NEW** - Revoke offline sessions on logout |
| **Protocol & Security** | | | |
| protocol | ✅ | ⚠️ | **NEW** - openid-connect or saml |
| pkceCodeChallengeMethod | ✅ | ⚠️ | **NEW** - PKCE code challenge (S256, plain) |
| **Token & Session Lifespan** | | | |
| accessTokenLifespan | ✅ | ⚠️ | **NEW** - Access token lifespan (seconds) |
| clientSessionIdleTimeout | ✅ | ⚠️ | **NEW** - Idle timeout for sessions |
| clientSessionMaxLifespan | ✅ | ⚠️ | **NEW** - Maximum session lifespan |
| clientOfflineSessionIdleTimeout | ✅ | ⚠️ | **NEW** - Offline session idle timeout |
| clientOfflineSessionMaxLifespan | ✅ | ⚠️ | **NEW** - Maximum offline session lifespan |
| **Advanced Features** | | | |
| authorizationServicesEnabled | ✅ | ⚠️ | **NEW** - Enable authorization services |
| oauth2DeviceAuthorizationGrantEnabled | ✅ | ⚠️ | **NEW** - Device authorization grant flow |
| standardTokenExchangeEnabled | ✅ | ⚠️ | **NEW** - Token exchange protocol |
| useRefreshTokens | ✅ | ⚠️ | **NEW** - Issue refresh tokens |
| **Secret Management** | | | |
| writeToSecretRef | ✅ | ⚠️ | **NEW** - Push K8s secret to Keycloak |
| readFromSecretRef | ✅ | ⚠️ | **NEW** - Pull generated secret to K8s |
| clientSecretSecretRef | ✅ | ✅ | Deprecated, use readFromSecretRef instead |

### Summary
- **✅ Fully compatible:** All core fields supported
- **✅ Enhanced:** 20+ new fields added for advanced configuration
- **✅ Backward compatible:** Existing manifests work unchanged

---

## Realm (realm.keycloak.m.crossplane.io/v1beta1)

### Field Support Matrix

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| realm | ✅ | ✅ | Realm name (unique) |
| displayName | ✅ | ✅ | Display name |
| enabled | ✅ | ✅ | Enable/disable realm |
| loginWithEmailAllowed | ✅ | ✅ | Allow login with email |
| sslRequired | ✅ | ✅ | SSL requirement level |
| themes | ✅ | ✅ | Theme configuration |
| smtp | ✅ | ✅ | SMTP settings |
| authFlows | ✅ | ✅ | Authentication flow configuration |

### Expansion Opportunities (Future)
- Password policies
- Session/token lifespan settings
- SAML/OIDC defaults
- User federation
- Internationalization

---

## User (user.keycloak.m.crossplane.io/v1beta1)

### Field Support Matrix

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| username | ✅ | ✅ | Unique username |
| email | ✅ | ✅ | User email |
| firstName | ✅ | ✅ | First name |
| lastName | ✅ | ✅ | Last name |
| enabled | ✅ | ✅ | Enable/disable user |
| emailVerified | ✅ | ✅ | Email verification status |
| requiredActions | ✅ | ✅ | Required actions (UPDATE_PASSWORD, etc.) |

### Expansion Opportunities (Future)
- User attributes map
- Credentials management
- Role assignment
- Group membership
- Authentication details

---

## Role (role.keycloak.m.crossplane.io/v1beta1)

### Field Support Matrix

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| name | ✅ | ✅ | Role name |
| description | ✅ | ✅ | Role description |
| realmId | ✅ | ✅ | Realm reference |
| composite | ✅ | ✅ | Composite role |
| containedRoles | ✅ | ✅ | Contained roles |

### Summary
- **✅ Complete:** All fields supported
- **Status:** Minimal expansion needed

---

## Group (user.keycloak.m.crossplane.io/v1beta1)

### Field Support Matrix

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| name | ✅ | ✅ | Group name |
| path | ✅ | ✅ | Group path |
| realmId | ✅ | ✅ | Realm reference |
| attributes | ✅ | ✅ | Group attributes |

### Summary
- **✅ Complete:** All fields supported

---

## Groups (user.keycloak.m.crossplane.io/v1beta1)

Group membership management for users.

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| userId | ✅ | ✅ | User reference |
| groupId | ✅ | ✅ | Group reference |

### Summary
- **✅ Complete:** All fields supported

---

## ProtocolMapper (client.keycloak.m.crossplane.io/v1beta1)

Protocol mapper configuration for clients.

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| name | ✅ | ✅ | Mapper name |
| protocol | ✅ | ✅ | Protocol type |
| protocolMapper | ✅ | ✅ | Mapper implementation |
| clientId | ✅ | ✅ | Client reference |
| config | ✅ | ✅ | Mapper configuration |

### Summary
- **✅ Complete:** All fields supported

---

## ClientDefaultScopes (openidclient.keycloak.m.crossplane.io/v1beta1)

Default scope assignment for clients.

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| clientId | ✅ | ✅ | Client reference |
| defaultScopes | ✅ | ✅ | Default scopes |

### Summary
- **✅ Complete:** All fields supported

---

## ClientOptionalScopes (openidclient.keycloak.m.crossplane.io/v1beta1)

Optional scope assignment for clients.

| Field | v0.1.0 | crossplane-contrib | Notes |
|-------|--------|-------------------|-------|
| clientId | ✅ | ✅ | Client reference |
| optionalScopes | ✅ | ✅ | Optional scopes |

### Summary
- **✅ Complete:** All fields supported

---

## Migration Guide

### From crossplane-contrib/provider-keycloak to provider-keycloak

1. **No manifest changes required** - All existing specs continue to work
2. **Update image reference** in your Kubernetes deployment
3. **Test in staging first** - Verify all resources work
4. **Gradual migration** - Update provider controller when ready

### Example Migration

```yaml
# Before (crossplane-contrib)
apiVersion: openidclient.keycloak.m.crossplane.io/v1beta1
kind: Client
metadata:
  name: my-app
spec:
  forProvider:
    clientId: my-app
    # ... existing config ...

# After (provider-keycloak) - No changes needed!
apiVersion: openidclient.keycloak.m.crossplane.io/v1beta1
kind: Client
metadata:
  name: my-app
spec:
  forProvider:
    clientId: my-app
    # ... same config ...
    # Can now add NEW fields if desired:
    homeUrl: "https://app.example.com/home"
    useRefreshTokens: true
```

---

## Version History

| Version | Base Provider | New Fields | Fixes |
|---------|---------------|-----------|-------|
| v0.1.0 | crossplane-contrib v2.19.0 | Client: 20+ fields | Bug #1: CRD groups |

---

## Support Matrix

| Resource | v0.1.0 | Production Ready | Notes |
|----------|--------|------------------|-------|
| Client | ✅ | ✅ | Full featured |
| Realm | ✅ | ✅ | Core functionality |
| User | ✅ | ✅ | Core functionality |
| Role | ✅ | ✅ | Simple, complete |
| Group | ✅ | ✅ | Simple, complete |
| Groups | ✅ | ✅ | Membership mgmt |
| ProtocolMapper | ✅ | ✅ | Mapper management |
| ClientDefaultScopes | ✅ | ✅ | Scope assignment |
| ClientOptionalScopes | ✅ | ✅ | Scope assignment |

**All resources:** ✅ Production ready
