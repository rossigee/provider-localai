# Experimental Admin API v2 Support (Keycloak 26.8+)

## Overview

The provider supports experimental Admin API v2, available in Keycloak 26.8.0 and later. This provides improved client management with better filtering and querying capabilities compared to v1.

## Prerequisites

- Keycloak 26.8.0 or later
- Keycloak server started with `--features=client-admin-api-v2`
- Provider credentials configured with `admin_api_v2_enabled: true`

## Enabling Admin API v2

### Keycloak Server Setup

Start the Keycloak server with the v2 client admin API feature enabled:

```bash
kc.sh start --features=client-admin-api-v2
```

Or via environment variable:
```bash
export KC_FEATURES=client-admin-api-v2
```

### Provider Configuration

Update the provider credentials secret to enable v2 support:

```json
{
  "url": "https://keycloak.example.com",
  "realm": "master",
  "client_id": "provider-admin",
  "client_secret": "your-secret",
  "admin_api_v2_enabled": true
}
```

## Behavior

### v2 Enabled

When `admin_api_v2_enabled: true`:
- Client queries use the v2 API endpoint `/admin/api/{realm}/clients/v2`
- Query expressions provide more flexible filtering
- Automatically falls back to v1 if v2 returns 404 (e.g., feature not enabled on server)

### v2 Disabled (Default)

When `admin_api_v2_enabled: false` (default):
- Client queries use the traditional v1 API endpoint `/admin/realms/{realm}/clients`
- Standard query parameters are used for filtering
- Full backward compatibility with all Keycloak versions

## Supported Methods

The following methods support v2 when enabled:

- `GetClient(ctx, realm, clientID)` — Query by clientId with v2's query expression syntax
- `ListClients(ctx, realm)` — List all clients using v2's paginated endpoint

## Automatic Fallback

If the server has `admin_api_v2_enabled: true` in provider credentials but the Keycloak server does not have the `client-admin-api-v2` feature enabled:

1. Provider attempts v2 API call
2. Server returns 404 (endpoint not found)
3. Provider automatically falls back to v1 API
4. Operation succeeds transparently

This ensures graceful degradation and backward compatibility.

## Query Syntax (v2)

The v2 API uses query expressions in the `q` parameter:

```
GET /admin/api/{realm}/clients/v2?q=clientId=my-app
```

The provider automatically constructs these expressions from method parameters.

## Migration from v1 to v2

No changes are required to your code:

1. Set `admin_api_v2_enabled: true` in credentials
2. Test with Keycloak 26.8.0+ with the feature enabled
3. Provider seamlessly uses v2 APIs where available
4. Existing code continues to work unchanged

## Limitations

The experimental v2 API has some known limitations:

- **Create/Update/Delete**: Still use v1 (only GetClient and ListClients use v2)
- **Sorting**: Not yet supported
- **ETags**: Not yet supported
- **Bulk Operations**: Not yet supported

These will likely be addressed in future Keycloak releases.

## Troubleshooting

### Provider uses v1 instead of v2

Check that:
1. `admin_api_v2_enabled` is set to `true` in credentials
2. Keycloak server is 26.8.0 or later
3. Keycloak was started with `--features=client-admin-api-v2`

### Query returns different results

v2 uses the same data as v1 — if results differ, it's likely a bug. Please report with:
- Keycloak version
- v2 query expression used
- v1 and v2 result comparison

## Feature Status

**Status**: Experimental (tracking Keycloak 26.8.0)

This feature may change as the upstream Admin API v2 evolves. Monitor Keycloak release notes for updates.

## See Also

- [Keycloak Admin API v2 Documentation](https://www.keycloak.org/admin-api/admin-api-v2)
- [Keycloak 26.8.0 Release Notes](https://www.keycloak.org/docs/26.8.0/release_notes/)
