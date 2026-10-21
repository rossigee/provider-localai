# Rate Limiting & Backoff Strategy

## Overview

Provider-keycloak implements automatic rate limit handling for HTTP 429 (Too Many Requests) responses from Keycloak. This prevents controller thrashing and enables graceful degradation under load.

### User-Facing Behavior

When you encounter rate limiting:
- Resources show reconciliation errors mentioning "rate limited"
- Provider automatically retries within seconds, respecting server `Retry-After` hints
- No manual intervention needed
- Resources become Ready once backoff window expires

### Common Causes

- High-frequency deployments creating/updating many resources simultaneously
- Keycloak server under load with rate limiting enabled
- Multiple controller instances hitting the same server
- Large batch operations processed concurrently

### How It Works (Simple)

1. **Detection**: HTTP 429 response received from Keycloak
2. **Backoff**: Wait 1-30 seconds (exponential or server-specified)
3. **Retry**: Automatic retry after backoff expires
4. **Recovery**: First successful response clears backoff state

No configuration needed—backoff is automatic and optimized for typical Keycloak deployments (10-30s rate limit windows).

## Implementation Details

### Architecture

The rate limiter is integrated at the HTTP client level (`internal/clients/keycloak.go`):

- **Level**: Transport layer (all HTTP methods)
- **State**: Per-client instance (thread-safe with mutex)
- **Scope**: Each `keycloakClient` maintains independent rate limit state

### Three HTTP Methods Protected

1. **`doRequest()`** - Main request handler for GET/PUT/DELETE operations
2. **`doCreate()`** - POST operations that create resources (returns Location header)
3. **`UpdateRealmRaw()`** - Raw realm update with JSON body

### Rate Limit Detection

Every HTTP response is checked for status code 429:

```go
if resp.StatusCode == http.StatusTooManyRequests {
    retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
    c.recordRateLimitHit(retryAfter)
    return nil, ErrRateLimited
}
```

### Backoff Calculation

#### With Retry-After Header

Server specifies backoff duration (RFC 7231):

```
Retry-After: 30           // 30 seconds (delta-seconds)
Retry-After: Fri, 21 Sep 2026 16:28:00 GMT  // HTTP-date format
```

Parsed value is capped at 30 seconds (rateLimitBackoffMax):

```go
if retryAfter > rateLimitBackoffMax {
    retryAfter = rateLimitBackoffMax  // Cap at 30s
}
```

#### Without Retry-After (Exponential Backoff)

Uses exponential backoff for consecutive hits:

```
Hit 1: 1s * 2^(1-1) = 1s
Hit 2: 1s * 2^(2-1) = 2s
Hit 3: 1s * 2^(3-1) = 4s
Hit 4: 1s * 2^(4-1) = 8s
Hit 5+: capped at 30s
```

Formula: `delay = rateLimitBackoffInitial << (consecutive-1)`

Constants:
- `rateLimitBackoffInitial` = 1 second
- `rateLimitBackoffMax` = 30 seconds

### Backoff Enforcement

Before each request, `checkRateLimitBackoff()` is called:

```go
if wait, err := c.checkRateLimitBackoff(); err != nil {
    return nil, ErrRateLimited  // Fail request if in backoff
}
```

Returns:
- `(0, nil)` if NOT in backoff window → request proceeds
- `(duration, ErrRateLimited)` if IN backoff → request rejected

### Backoff Clearing

Backoff state is cleared ONLY on successful (2xx) responses:

```go
if resp.StatusCode >= 200 && resp.StatusCode < 300 {
    c.clearRateLimitBackoff()
    return respBody, nil
}
```

This ensures:
- Single successful request clears the backoff window
- Consecutive failures extend the backoff
- No premature retry attempts

## Error Handling

### ErrRateLimited

Distinct error type signals rate limiting to controllers:

```go
var ErrRateLimited = errors.New("Keycloak rate limited")
```

Controllers should detect and handle:

```go
resp, err := client.GetClient(ctx, realm, clientID)
if errors.Is(err, clients.ErrRateLimited) {
    // Apply backoff - error message contains duration
    return managed.ExternalObservation{}, managed.ExternalError{
        Err: err,
        RequeueAfter: parseDurationFromError(err),
    }
}
```

### Error Message Format

```
Keycloak rate limited: request failed with status 429: rate limited, Retry-After 30s until 2026-09-20T16:28:30Z
```

Includes:
- Status code (429)
- Backoff method (Retry-After vs exponential)
- Target time when backoff expires

## Thread Safety

All rate limit operations are protected by `sync.Mutex`:

```go
kc.mu.Lock()
defer kc.mu.Unlock()
```

Protects:
- `rateLimitUntil` - Backoff expiration time
- `rateLimitConsecutive` - Hit counter for exponential backoff

Separate from token mutex for independent tracking.

## Test Coverage

Comprehensive test suite in `keycloak_test.go`:

- `TestParseRetryAfter` - Header parsing (delta-seconds, HTTP-date, invalid)
- `TestRateLimitBackoffTracking` - Basic backoff state transitions
- `TestRateLimitExponentialBackoff` - Exponential backoff progression
- `TestRateLimitRetryAfterCap` - 30s cap enforcement
- `TestDoRequestHandles429` - Full integration (doRequest path)
- `TestDoCreateHandles429` - Full integration (doCreate path)

Coverage: 41.5% of clients package

## Deployment Scenarios

### Scenario 1: Slow Keycloak Server
- Server responds slowly (queueing requests)
- Applies rate limit to protect itself
- Provider receives 429 with Retry-After: 10s
- Waits 10s before retry
- Request succeeds
- Backoff clears immediately

### Scenario 2: Rapid Deployment
- Controller creates 10 resources rapidly
- First fails with 429
- Backoff: 1s → no retry yet
- Subsequent attempts fail due to active backoff
- Wait 1s, resource succeeds, backoff cleared
- Remaining resources proceed normally

### Scenario 3: Persistent Overload
- Server consistently rate-limited
- Each 429 increases backoff: 1s → 2s → 4s
- After 3-4 hits, backoff reaches 8-16s
- Provides natural pressure relief
- Once resolved, single success clears state

## Configuration

No configuration needed! Backoff parameters are optimized for:
- Typical Keycloak deployments
- 10-30 second rate limit windows at proxy layers
- Kubernetes reconciliation loops (5-30s intervals)

If different behavior is needed, tune these constants in `keycloak.go`:

```go
const (
    rateLimitBackoffInitial = 1 * time.Second   // Starting backoff
    rateLimitBackoffMax     = 30 * time.Second  // Maximum backoff
)
```

## Observability

### Logs
Check controller logs for rate limiting:

```bash
kubectl logs -n crossplane-system deployment/provider-keycloak-controller | grep "rate limited"
```

### Metrics
Rate limit events are exposed via errors:

```
- ErrRateLimited count: Number of 429 responses
- Backoff duration: Visible in error messages
- Retry timing: Calculated from error message timestamp
```

### Events
Resource condition will show status during backoff:

```bash
kubectl describe realm my-realm
```

Will show reconciliation errors with rate limit details during backoff.

## Future Enhancements

Potential improvements (not currently implemented):

1. **Metrics export** - Prometheus metrics for rate limit events
2. **Per-resource backoff** - Individual backoff windows per resource
3. **Adaptive backoff** - ML-based prediction of rate limit windows
4. **Circuit breaker** - Temporary upstream fallback on persistent rate limiting
5. **Rate limiting awareness** - Intentional request throttling based on load prediction

## References

- [RFC 7231 - Retry-After](https://tools.ietf.org/html/rfc7231#section-7.1.3)
- [HTTP 429 Status Code](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/429)
- [Exponential Backoff](https://en.wikipedia.org/wiki/Exponential_backoff)
