# Provider-Keycloak API Reference

Complete reference for all 21 managed resources provided by provider-keycloak.

## Table of Contents

- [Core Resources](#core-resources)
- [User & Group Management](#user--group-management)
- [Authorization & Access Control](#authorization--access-control)
- [Client Configuration](#client-configuration)
- [Identity & Authentication](#identity--authentication)
- [Infrastructure & Administration](#infrastructure--administration)

---

## Core Resources

### Realm

Manages Keycloak realms (isolated authentication domains).

**Kind**: `Realm`  
**Group**: `realm.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `name` (required): Realm name
- `enabled`: Enable/disable realm (default: true)
- `displayName`: Human-readable realm name
- `displayNameHtml`: HTML-formatted display name
- `themes`: Theme configuration (login, account, admin)
- `smtpServer`: Email configuration

**Example**:
```yaml
apiVersion: realm.keycloak.m.crossplane.io/v1beta1
kind: Realm
metadata:
  name: my-realm
spec:
  forProvider:
    name: myrealm
    displayName: My Organization
    enabled: true
  providerConfigRef:
    name: keycloak
```

### Client (OpenID Connect)

Manages Keycloak OIDC clients for application authentication.

**Kind**: `Client`  
**Group**: `openidclient.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): OIDC client ID
- `name`: Human-readable client name
- `rootUrl`: Root URL for redirects
- `validRedirectUris`: Allowed redirect URLs
- `webOrigins`: Allowed CORS origins
- `standardFlowEnabled`: Enable authorization code flow
- `implicitFlowEnabled`: Enable implicit flow
- `directAccessGrantsEnabled`: Enable Resource Owner Password flow
- `serviceAccountsEnabled`: Enable service account flow

**Example**:
```yaml
apiVersion: openidclient.keycloak.m.crossplane.io/v1beta1
kind: Client
metadata:
  name: web-app
spec:
  forProvider:
    realmId: myrealm
    clientId: web-app
    name: Web Application
    rootUrl: https://app.example.com
    validRedirectUris:
      - "https://app.example.com/callback"
    standardFlowEnabled: true
  providerConfigRef:
    name: keycloak
```

---

## User & Group Management

### User

Manages Keycloak users within a realm.

**Kind**: `User`  
**Group**: `user.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `username` (required): User login name
- `email`: User email address
- `firstName`: First name
- `lastName`: Last name
- `enabled`: Enable/disable user (default: true)
- `emailVerified`: Mark email as verified

**Example**:
```yaml
apiVersion: user.keycloak.m.crossplane.io/v1beta1
kind: User
metadata:
  name: john-user
spec:
  forProvider:
    realmId: myrealm
    username: john.doe
    email: john@example.com
    firstName: John
    lastName: Doe
    enabled: true
  providerConfigRef:
    name: keycloak
```

### Group

Manages Keycloak user groups for role inheritance.

**Kind**: `Group`  
**Group**: `user.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `name` (required): Group name
- `path`: Full group path (auto-generated)
- `attributes`: Custom group attributes

**Example**:
```yaml
apiVersion: user.keycloak.m.crossplane.io/v1beta1
kind: Group
metadata:
  name: admins-group
spec:
  forProvider:
    realmId: myrealm
    name: admins
    attributes:
      department: IT
  providerConfigRef:
    name: keycloak
```

---

## Authorization & Access Control

### Role (Realm-scoped)

Manages realm-level roles assigned across all clients.

**Kind**: `Role`  
**Group**: `role.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `name` (required): Role name
- `description`: Role description
- `composite`: Is this a composite role

**Example**:
```yaml
apiVersion: role.keycloak.m.crossplane.io/v1beta1
kind: Role
metadata:
  name: admin-role
spec:
  forProvider:
    realmId: myrealm
    name: admin
    description: Administrator role
  providerConfigRef:
    name: keycloak
```

### Role (Client-scoped)

Manages client-specific roles within a Keycloak client.

**Kind**: `Role`  
**Group**: `role.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Target client UUID
- `name` (required): Role name
- `description`: Role description

**Note**: Use `clientId` field to distinguish client roles from realm roles.

### AuthorizationResource

Manages UMA (User-Managed Access) resources for fine-grained authorization.

**Kind**: `AuthorizationResource`  
**Group**: `authz.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Resource server client
- `name` (required): Resource name
- `displayName`: Human-readable name
- `uris`: Resource URIs
- `scopes`: Associated scopes
- `attributes`: Custom attributes

**Example**:
```yaml
apiVersion: authz.keycloak.m.crossplane.io/v1beta1
kind: AuthorizationResource
metadata:
  name: protected-api
spec:
  forProvider:
    realmId: myrealm
    clientId: api-client-uuid
    name: protected-api
    uris:
      - "/*"
    scopes:
      - read
      - write
  providerConfigRef:
    name: keycloak
```

### AuthorizationPolicy

Manages authorization policies for resource access decisions.

**Kind**: `AuthorizationPolicy`  
**Group**: `authorizationpolicy.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Resource server client
- `name` (required): Policy name
- `type` (required): Policy type (role, user, resource, scope, aggregate)
- `description`: Policy description
- `logic`: Policy logic (POSITIVE or NEGATIVE)
- `config`: Policy-specific configuration

**Supported Policy Types**:
- `role`: Grant/deny based on user roles
- `user`: Grant/deny based on user attributes
- `resource`: Grant/deny based on resources
- `scope`: Grant/deny based on scopes
- `aggregate`: Combine multiple policies

**Example**:
```yaml
apiVersion: authorizationpolicy.keycloak.m.crossplane.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: admin-policy
spec:
  forProvider:
    realmId: myrealm
    clientId: api-client-uuid
    name: admin-policy
    type: role
    logic: POSITIVE
    config:
      roles: "admin"
  providerConfigRef:
    name: keycloak
```

---

## Client Configuration

### ProtocolMapper

Configures how client claims are transformed and mapped.

**Kind**: `ProtocolMapper`  
**Group**: `client.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Target client
- `name` (required): Mapper name
- `protocol` (required): Protocol (openid-connect, saml)
- `protocolMapper` (required): Mapper implementation
- `config`: Mapper-specific configuration

**Common Mappers**:
- `oidc-userinfo-json-file-mapper`: Map userinfo from JSON
- `oidc-address-mapper`: Map address claims
- `oidc-full-name-mapper`: Map full name claim
- `saml-role-list-mapper`: Map SAML role lists

### ClientRoleMapping

Assigns client-scoped roles to users.

**Kind**: `ClientRoleMapping`  
**Group**: `rolemappings.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `userId` (required): User UUID
- `clientId` (required): Client UUID
- `roles` (required): Role names to assign

**Example**:
```yaml
apiVersion: rolemappings.keycloak.m.crossplane.io/v1beta1
kind: ClientRoleMapping
metadata:
  name: user-client-roles
spec:
  forProvider:
    realmId: myrealm
    userId: user-uuid
    clientId: client-uuid
    roles:
      - app-admin
      - app-user
  providerConfigRef:
    name: keycloak
```

### ClientScopeMapping

Assigns realm-level scopes to a client.

**Kind**: `ClientScopeMapping`  
**Group**: `scopes.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Client UUID
- `scopes` (required): Scope names

### ClientDefaultScopes

Sets default OAuth2 scopes returned for client authorization requests.

**Kind**: `ClientDefaultScopes`  
**Group**: `openidclient.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Client UUID
- `defaultScopes`: List of default scope names

**Example**:
```yaml
apiVersion: openidclient.keycloak.m.crossplane.io/v1beta1
kind: ClientDefaultScopes
metadata:
  name: app-default-scopes
spec:
  forProvider:
    realmId: myrealm
    clientId: app-client-uuid
    defaultScopes:
      - openid
      - profile
      - email
  providerConfigRef:
    name: keycloak
```

### ClientOptionalScopes

Defines optional OAuth2 scopes clients can request.

**Kind**: `ClientOptionalScopes`  
**Group**: `openidclient.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Client UUID
- `optionalScopes`: List of optional scope names

---

## Identity & Authentication

### IdentityProvider

Manages SAML/OIDC identity providers for user federation.

**Kind**: `IdentityProvider`  
**Group**: `identityprovider.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `alias` (required): Provider alias
- `displayName`: Human-readable name
- `providerId` (required): Provider type (oidc, saml, etc)
- `enabled`: Enable/disable provider
- `trustEmail`: Trust email from provider
- `firstBrokerLoginFlowAlias`: Flow on first login
- `postBrokerLoginFlowAlias`: Flow after broker login
- `config`: Provider-specific configuration

**Common Providers**:
- `oidc`: OpenID Connect provider
- `saml`: SAML 2.0 provider
- `google`: Google OAuth
- `facebook`: Facebook OAuth
- `github`: GitHub OAuth

**Example (OIDC)**:
```yaml
apiVersion: identityprovider.keycloak.m.crossplane.io/v1beta1
kind: IdentityProvider
metadata:
  name: okta-provider
spec:
  forProvider:
    realmId: myrealm
    alias: okta
    displayName: Okta
    providerId: oidc
    enabled: true
    trustEmail: true
    config:
      clientId: okta-client-id
      clientSecret: okta-client-secret
      authorizationUrl: https://org.okta.com/oauth2/v1/authorize
      tokenUrl: https://org.okta.com/oauth2/v1/token
      userInfoUrl: https://org.okta.com/oauth2/v1/userinfo
  providerConfigRef:
    name: keycloak
```

### AuthenticationFlow

Manages authentication execution chains for login/registration.

**Kind**: `AuthenticationFlow`  
**Group**: `authenticationflow.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `alias` (required): Flow alias
- `providerId` (required): Flow type (basic-flow, client-flow)
- `description`: Flow description
- `builtIn`: Is built-in flow
- `topLevel`: Is top-level flow

**Built-in Flow Types**:
- `basic-flow`: Standard login flow
- `client-flow`: Client credentials flow
- `direct-grant`: Resource Owner Password flow
- `first-broker-login`: Federation first login

---

## Infrastructure & Administration

### UserFederationProvider

Integrates external user directories (LDAP, Kerberos, etc).

**Kind**: `UserFederationProvider`  
**Group**: `userfederation.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `providerId` (required): Provider type (ldap, kerberos, etc)
- `name`: Provider name
- `config`: Provider-specific configuration (LDAP URL, bind DN, etc)
- `priority`: Priority order for provider

### Component

Manages realm components (LDAP providers, key providers, etc).

**Kind**: `Component`  
**Group**: `component.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `name` (required): Component name
- `providerType` (required): Component type (org.keycloak.storage.UserStorageProvider, etc)
- `providerId`: Specific provider
- `config`: Component configuration

### ClientInitialAccess

Generates client registration tokens for programmatic client creation.

**Kind**: `ClientInitialAccess`  
**Group**: `clientinitialaccess.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `count` (required): Number of tokens to generate
- `expiration` (required): Token expiration (minutes)

### ClientCertificates

Manages client certificates for mutual TLS authentication.

**Kind**: `ClientCertificates`  
**Group**: `clientcertificates.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `clientId` (required): Client UUID
- `certificateId`: Certificate identifier
- `certificate`: PEM-encoded certificate

### RealmEventsConfig

Configures realm event logging and auditing.

**Kind**: `RealmEventsConfig`  
**Group**: `events.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm
- `eventsEnabled`: Enable event logging
- `eventsExpiration`: Event retention (seconds)
- `eventsListeners`: Event listeners
- `adminEventsEnabled`: Enable admin event logging
- `adminEventsDetails`: Log admin event details

### RealmImport

Imports realm configuration from JSON (read-only operation).

**Kind**: `RealmImport`  
**Group**: `realmimpexp.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmJSON` (required): Base64-encoded realm JSON
- `ifNotExists`: Only import if realm doesn't exist

### RealmKeys

Reads realm cryptographic keys (read-only).

**Kind**: `RealmKeys`  
**Group**: `keys.keycloak.m.crossplane.io`  
**Scope**: Namespaced

**Key Fields**:
- `realmId` (required): Target realm

---

## Common Patterns

### Cross-realm Resource References

Most resources require explicit `realmId`:
```yaml
spec:
  forProvider:
    realmId: production  # Must match actual realm name
    # other fields...
```

### Provider Configuration

All resources require ProviderConfig reference:
```yaml
spec:
  providerConfigRef:
    name: keycloak  # Name of ProviderConfig in same namespace
```

### Annotation-based ID Storage

Resources without built-in ID fields use annotations:
```yaml
metadata:
  annotations:
    crossplane.io/external-name: "resource-id-from-keycloak"
```

### Error Recovery

On reconciliation failure:
- Status condition: `Synced=False, SyncError=<error>`
- Check ProviderConfig availability
- Verify Keycloak connectivity
- Review resource parameters

---

## API Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 204 | No content |
| 400 | Bad request (validation error) |
| 401 | Unauthorized (auth error) |
| 403 | Forbidden (permission error) |
| 404 | Not found |
| 409 | Conflict (duplicate) |
| 500 | Internal server error |

---

## Additional Resources

- [Keycloak Official Documentation](https://www.keycloak.org/documentation)
- [Crossplane Provider Development](https://crossplane.io/docs/latest/concepts/providers)
- [OAuth2/OIDC Specifications](https://openid.net/connect/)
- [SAML 2.0 Standard](https://en.wikipedia.org/wiki/SAML_2.0)
