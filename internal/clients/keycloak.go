/*
Copyright 2024 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clients

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rossigee/provider-keycloak/apis/v1beta1"
)

const (
	defaultTimeout       = 30 * time.Second
	adminPath            = "/admin/realms"
	adminAPIv2Path       = "/admin/api" // experimental: Keycloak 26.8+ Admin API v2
	oauthKeyClientID     = "client_id"
	oauthKeyClientSecret = "client_secret"
	maxErrBodyLen        = 256
)

// Backoff schedule for token acquisition failures. We hold the cached
// failure for at most backoffMax so a transient outage clears quickly.
const (
	backoffInitial = 5 * time.Second
	backoffMax     = 5 * time.Minute
)

// Rate limit backoff schedule for API errors (HTTP 429).
// Uses a separate, shorter schedule than token acquisition since rate limit
// windows are typically 10-30 seconds at the proxy layer.
const (
	rateLimitBackoffInitial = 1 * time.Second
	rateLimitBackoffMax     = 30 * time.Second
	// rateLimitBackoffJitter adds randomness to backoff deadline to prevent
	// thundering herd when multiple reconcilers all retry at the same instant
	// the backoff window expires.
	rateLimitBackoffJitter = 2 * time.Second
)

// debugHTTP enables verbose request/response logging in doRequest, gated by
// an env var so it can be toggled without a code change. Temporary
// diagnostic aid.
var debugHTTP = os.Getenv("KEYCLOAK_PROVIDER_DEBUG_HTTP") == "true"

// RateLimitError indicates the Keycloak server returned HTTP 429 and we're
// applying exponential backoff. It includes the deadline when the backoff
// window expires, allowing callers to respect this specific deadline rather
// than applying generic backoff.
type RateLimitError struct {
	deadline time.Time
	wait     time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit backoff until %s (wait %v): Keycloak rate limited", e.deadline.Format(time.RFC3339), e.wait)
}

// Deadline returns the time when the rate limit backoff window expires.
func (e *RateLimitError) Deadline() time.Time {
	return e.deadline
}

// RequeueAfter returns the duration to wait before retrying.
func (e *RateLimitError) RequeueAfter() time.Duration {
	return e.wait
}

// sensitiveJSONFieldRe matches credential-bearing string fields in a JSON body.
// Keycloak's admin API is camelCase, but the underscore forms are included so a
// snake_case field cannot slip past by being spelled differently.
//
// The list is deliberately limited to fields whose value *is* the secret. A
// catch-all such as "value" would redact ordinary data and leave the dump
// useless, so a nested Keycloak credentials array ({"type":"password",
// "value":...}) is out of scope: this provider's request types have no
// Credentials field, so it never emits one.
var sensitiveJSONFieldRe = regexp.MustCompile(`(?i)"(password|secret|client_?secret|private_?key|temporary_?password|access_?token|refresh_?token|id_?token|token)"\s*:\s*"[^"]*"`)

// redactSensitiveJSON replaces the value of every credential-bearing field with
// REDACTED, keeping the field name so the shape of the payload stays visible.
func redactSensitiveJSON(b []byte) []byte {
	return sensitiveJSONFieldRe.ReplaceAll(b, []byte(`"$1":"REDACTED"`))
}

// sensitiveHeaders are the headers that carry a credential and must never reach
// a log. Keycloak's own bearer token is the important one; the rest are included
// because the same request path would leak them if they were ever set.
var sensitiveHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Authorization",
}

// redactHeaders returns a copy of h with every credential-bearing value
// replaced. The input is not modified, so the caller's request is untouched.
func redactHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := h.Clone()
	for _, name := range sensitiveHeaders {
		if out.Get(name) != "" {
			out.Set(name, "REDACTED")
		}
	}
	return out
}

// countingReader wraps a reader and tracks how many bytes have been Read,
// to check whether the HTTP transport actually consumes the full body.
// Temporary diagnostic aid.
type countingReader struct {
	r *bytes.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// ErrAuthUnavailable indicates that an access token could not be obtained
// from Keycloak and the provider is currently in its backoff window.
// Controllers should map this to a RequeueAfter rather than letting it
// bubble as an unrecoverable reconcile error.
var ErrAuthUnavailable = errors.New("Keycloak authentication unavailable")

// ErrRateLimited indicates that the Keycloak server returned HTTP 429 Too
// Many Requests. The caller should apply the backoff specified in the error
// message rather than immediately retrying.
var ErrRateLimited = errors.New("Keycloak rate limited")

// ErrNotFound indicates Keycloak returned HTTP 404 for the requested resource.
//
// Callers must distinguish this from a transport failure. A managed reconciler
// aborts when Observe returns an error, so treating "absent" as an error stops
// it ever creating a missing resource, or releasing a finalizer for one whose
// parent has been deleted.
var ErrNotFound = errors.New("Keycloak resource not found")

// notFoundError preserves the historical message shape - several callers still
// match on the "404" substring - while allowing errors.Is(err, ErrNotFound).
type notFoundError struct {
	status int
	body   string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("request failed with status %d: %s", e.status, e.body)
}

func (e *notFoundError) Unwrap() error { return ErrNotFound }

// realmPath returns the safely encoded admin API path for a realm.
func realmPath(realm string) string {
	return adminPath + "/" + url.PathEscape(realm)
}

// Client interface for Keycloak API operations
type Client interface {
	// Realm operations
	GetRealm(ctx context.Context, realm string) (*Realm, error)
	CreateRealm(ctx context.Context, realm *Realm) (*Realm, error)
	UpdateRealm(ctx context.Context, realm *Realm) error
	DeleteRealm(ctx context.Context, realm string) error

	// Client operations
	// GetClient and ListClients support experimental Admin API v2 (Keycloak 26.8+)
	// when admin_api_v2_enabled is set in credentials. Falls back to v1 if v2 is unavailable.
	GetClient(ctx context.Context, realm, clientID string) (*ClientRepresentation, error)
	CreateClient(ctx context.Context, realm string, client *ClientRepresentation) (*ClientRepresentation, error)
	UpdateClient(ctx context.Context, realm string, client *ClientRepresentation) error
	DeleteClient(ctx context.Context, realm, clientID string) error
	ListClients(ctx context.Context, realm string) ([]ClientRepresentation, error)

	// User operations
	GetUser(ctx context.Context, realm, username string) (*UserRepresentation, error)
	CreateUser(ctx context.Context, realm string, user *UserRepresentation) (*UserRepresentation, error)
	UpdateUser(ctx context.Context, realm string, user *UserRepresentation) error
	DeleteUser(ctx context.Context, realm, userID string) error
	ListUsers(ctx context.Context, realm string) ([]UserRepresentation, error)

	// Client secret operations
	GetClientSecret(ctx context.Context, realm, clientUUID string) (string, error)
	ResetClientSecret(ctx context.Context, realm, clientUUID string) error

	// Group operations
	GetGroup(ctx context.Context, realm, groupID string) (*GroupRepresentation, error)
	CreateGroup(ctx context.Context, realm string, group *GroupRepresentation) (*GroupRepresentation, error)
	UpdateGroup(ctx context.Context, realm string, group *GroupRepresentation) error
	DeleteGroup(ctx context.Context, realm, groupID string) error
	ListGroups(ctx context.Context, realm string) ([]GroupRepresentation, error)
	SearchGroups(ctx context.Context, realm, name string) ([]GroupRepresentation, error)

	// User group membership operations
	GetUserGroups(ctx context.Context, realm, userUUID string) ([]GroupRepresentation, error)
	AddUserToGroup(ctx context.Context, realm, userUUID, groupUUID string) error
	RemoveUserFromGroup(ctx context.Context, realm, userUUID, groupUUID string) error
	SearchUsers(ctx context.Context, realm, username string) ([]UserRepresentation, error)

	// Role operations (realm-scoped)
	GetRealmRole(ctx context.Context, realm, name string) (*RoleRepresentation, error)
	CreateRealmRole(ctx context.Context, realm string, role *RoleRepresentation) error
	UpdateRealmRole(ctx context.Context, realm, name string, role *RoleRepresentation) error
	DeleteRealmRole(ctx context.Context, realm, name string) error

	// Role operations (client-scoped)
	GetClientRole(ctx context.Context, realm, clientUUID, name string) (*RoleRepresentation, error)
	CreateClientRole(ctx context.Context, realm, clientUUID string, role *RoleRepresentation) error
	UpdateClientRole(ctx context.Context, realm, clientUUID, name string, role *RoleRepresentation) error
	DeleteClientRole(ctx context.Context, realm, clientUUID, name string) error

	// Protocol mapper operations
	GetClientProtocolMapper(ctx context.Context, realm, clientUUID, mapperID string) (*ProtocolMapperRepresentation, error)
	CreateClientProtocolMapper(ctx context.Context, realm, clientUUID string, mapper *ProtocolMapperRepresentation) (string, error)
	UpdateClientProtocolMapper(ctx context.Context, realm, clientUUID string, mapper *ProtocolMapperRepresentation) error
	DeleteClientProtocolMapper(ctx context.Context, realm, clientUUID, mapperID string) error
	ListClientProtocolMappers(ctx context.Context, realm, clientUUID string) ([]ProtocolMapperRepresentation, error)

	// User Federation operations
	GetUserFederationProvider(ctx context.Context, realm, providerID string) (*UserFederationProviderRepresentation, error)
	CreateUserFederationProvider(ctx context.Context, realm string, provider *UserFederationProviderRepresentation) (string, error)
	UpdateUserFederationProvider(ctx context.Context, realm, providerID string, provider *UserFederationProviderRepresentation) error
	DeleteUserFederationProvider(ctx context.Context, realm, providerID string) error
	ListUserFederationProviders(ctx context.Context, realm string) ([]UserFederationProviderRepresentation, error)

	// Events configuration operations
	GetRealmEventsConfig(ctx context.Context, realm string) (*RealmEventsConfigRepresentation, error)
	UpdateRealmEventsConfig(ctx context.Context, realm string, config *RealmEventsConfigRepresentation) error

	// Realm Import operations
	ImportRealm(ctx context.Context, realmJSON string, ifNotExists bool) error

	// Authorization (UMA) operations
	GetAuthzResource(ctx context.Context, realm, clientID, resourceID string) (*AuthzResourceRepresentation, error)
	CreateAuthzResource(ctx context.Context, realm, clientID string, resource *AuthzResourceRepresentation) (string, error)
	UpdateAuthzResource(ctx context.Context, realm, clientID, resourceID string, resource *AuthzResourceRepresentation) error
	DeleteAuthzResource(ctx context.Context, realm, clientID, resourceID string) error
	ListAuthzResources(ctx context.Context, realm, clientID string) ([]AuthzResourceRepresentation, error)

	// Client Certificate operations
	GetClientCertificate(ctx context.Context, realm, clientID, certID string) (*ClientCertificateRepresentation, error)
	GenerateClientCertificate(ctx context.Context, realm, clientID string, format string) (*ClientCertificateRepresentation, error)
	ListClientCertificates(ctx context.Context, realm, clientID string) ([]ClientCertificateRepresentation, error)

	// Role Mapping operations (user-to-client-role assignments)
	ListUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string) ([]RoleRepresentation, error)
	AddUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string, roles []RoleRepresentation) error
	RemoveUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string, roles []RoleRepresentation) error

	// Client Scope Mapping operations (realm-level scopes for a client)
	ListClientScopeMappings(ctx context.Context, realm, clientUUID string) ([]RoleRepresentation, error)
	AddClientScopeMappings(ctx context.Context, realm, clientUUID string, scopes []RoleRepresentation) error
	RemoveClientScopeMappings(ctx context.Context, realm, clientUUID string, scopes []RoleRepresentation) error

	// Client Initial Access operations
	CreateClientInitialAccess(ctx context.Context, realm string, count, expiration int32) (*ClientInitialAccessRepresentation, error)
	ListClientInitialAccess(ctx context.Context, realm string) ([]ClientInitialAccessRepresentation, error)
	DeleteClientInitialAccess(ctx context.Context, realm, id string) error

	// Component operations
	GetComponent(ctx context.Context, realm, id string) (*ComponentRepresentation, error)
	CreateComponent(ctx context.Context, realm string, c *ComponentRepresentation) (string, error)
	UpdateComponent(ctx context.Context, realm, id string, c *ComponentRepresentation) error
	DeleteComponent(ctx context.Context, realm, id string) error
	ListComponentsByType(ctx context.Context, realm, providerType, name string) ([]ComponentRepresentation, error)

	// Realm Keys operations (read-only)
	GetRealmKeys(ctx context.Context, realm string) (*RealmKeysRepresentation, error)

	// Client Default Scopes operations
	ListClientDefaultScopes(ctx context.Context, realm, clientUUID string) ([]ClientScopeRepresentation, error)
	AddClientDefaultScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error
	RemoveClientDefaultScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error

	// Client Optional Scopes operations
	ListClientOptionalScopes(ctx context.Context, realm, clientUUID string) ([]ClientScopeRepresentation, error)
	AddClientOptionalScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error
	RemoveClientOptionalScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error

	// Client Scope definition operations (create/read/update/delete scope definitions)
	GetClientScope(ctx context.Context, realm, name string) (*ClientScopeRepresentation, error)
	CreateClientScope(ctx context.Context, realm string, scope ClientScopeRepresentation) error
	UpdateClientScope(ctx context.Context, realm string, scope ClientScopeRepresentation) error
	DeleteClientScope(ctx context.Context, realm, name string) error

	// Identity Provider operations
	GetIdentityProvider(ctx context.Context, realm, alias string) (*IdentityProviderRepresentation, error)
	CreateIdentityProvider(ctx context.Context, realm string, provider *IdentityProviderRepresentation) (string, error)
	UpdateIdentityProvider(ctx context.Context, realm, alias string, provider *IdentityProviderRepresentation) error
	DeleteIdentityProvider(ctx context.Context, realm, alias string) error
	ListIdentityProviders(ctx context.Context, realm string) ([]IdentityProviderRepresentation, error)

	// Authentication Flow operations
	GetAuthenticationFlow(ctx context.Context, realm, alias string) (*AuthenticationFlowRepresentation, error)
	CreateAuthenticationFlow(ctx context.Context, realm string, flow *AuthenticationFlowRepresentation) (string, error)
	UpdateAuthenticationFlow(ctx context.Context, realm, alias string, flow *AuthenticationFlowRepresentation) error
	DeleteAuthenticationFlow(ctx context.Context, realm, alias string) error
	ListAuthenticationFlows(ctx context.Context, realm string) ([]AuthenticationFlowRepresentation, error)

	// Authorization Policy operations
	GetAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string) (*AuthorizationPolicyRepresentation, error)
	CreateAuthorizationPolicy(ctx context.Context, realm, clientID string, policy *AuthorizationPolicyRepresentation) (string, error)
	UpdateAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string, policy *AuthorizationPolicyRepresentation) error
	DeleteAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string) error
	ListAuthorizationPolicies(ctx context.Context, realm, clientID string) ([]AuthorizationPolicyRepresentation, error)

	// Raw realm operations (for preserving all fields during updates)
	GetRawRealm(ctx context.Context, realm string) ([]byte, error)
	UpdateRealmRaw(ctx context.Context, realm string, realmJSON []byte) error
}

// keycloakClient implements Client
type keycloakClient struct {
	mu         sync.Mutex
	httpClient *http.Client
	baseURL    string
	token      string
	tokenExp   time.Time // token expiration time
	cfg        *Config   // for token refresh

	// Failure tracking for token acquisition backoff.
	lastFailure      error
	lastFailureAt    time.Time
	backoffUntil     time.Time
	consecutiveFails int

	// Failure tracking for API rate limit backoff (HTTP 429).
	rateLimitUntil       time.Time
	rateLimitConsecutive int

	// requestSemaphore limits concurrent API requests to prevent thundering
	// herd on 429 errors. With N concurrent reconcile workers all passing
	// the rate-limit backoff check simultaneously, they all hit a 429
	// together, causing the rate limit backoff to reset. A buffered channel
	// ensures only ~3 requests are in-flight at once.
	requestSemaphore chan struct{}
}

// NewClient creates a new Keycloak API client using OAuth2 client credentials.
func NewClient(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) (*keycloakClient, error) {
	cfg, err := GetConfig(ctx, pc, kube)
	if err != nil {
		return nil, errors.Wrap(err, "cannot load provider config")
	}
	return NewClientFromConfig(ctx, cfg)
}

// NewClientFromConfig creates a new Keycloak API client from a resolved Config.
func NewClientFromConfig(ctx context.Context, cfg *Config) (*keycloakClient, error) {
	// Use a dedicated Transport per client rather than the shared
	// http.DefaultTransport singleton. With many concurrent reconcile
	// workers hitting the same Keycloak host, DefaultTransport's low
	// MaxIdleConnsPerHost (2) causes heavy connection reuse under load;
	// a keep-alive connection reused across concurrent goroutines is a
	// known source of stream-framing desync between client and server,
	// which surfaces here as Keycloak-side "unable to read contents from
	// stream" errors on POST bodies with no corresponding server-side
	// application log entry (the request never reaches Keycloak's own
	// routing - it fails at the HTTP transport layer). Disabling
	// keep-alives trades a little latency for eliminating that entirely.
	dedicatedTransport := &http.Transport{
		DisableKeepAlives: true,
	}
	if cfg.RootCACertificate != "" || cfg.TLSInsecureSkipVerify {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: cfg.TLSInsecureSkipVerify, // nolint:gosec // configured via explicit provider credential
		}
		if cfg.RootCACertificate != "" {
			pool := x509.NewCertPool()
			if ok := pool.AppendCertsFromPEM([]byte(cfg.RootCACertificate)); !ok {
				return nil, errors.New("failed to parse root CA certificate PEM: no valid PEM blocks found")
			}
			tlsConfig.RootCAs = pool
		}
		dedicatedTransport.TLSClientConfig = tlsConfig
	}
	transport := http.RoundTripper(dedicatedTransport)

	httpClient := &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}

	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")

	token, exp, err := fetchOAuth2Token(ctx, httpClient, baseURL, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot obtain access token")
	}

	return &keycloakClient{
		httpClient:       httpClient,
		baseURL:          baseURL,
		token:            token,
		tokenExp:         exp,
		cfg:              cfg,
		requestSemaphore: make(chan struct{}, 3), // Allow ~3 concurrent requests
	}, nil
}

// tokenResponse is the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // token lifetime in seconds
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// fetchOAuth2Token obtains an access token via the client credentials grant.
// Returns the token and its expiration time.
func fetchOAuth2Token(ctx context.Context, hc *http.Client, baseURL string, cfg *Config) (string, time.Time, error) {
	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", baseURL, url.PathEscape(cfg.Realm))

	var form url.Values
	// Use password grant if username/password are provided, otherwise use client_credentials
	if cfg.Username != "" && cfg.Password != "" {
		form = url.Values{
			"grant_type":     {"password"},
			"username":       {cfg.Username},
			"password":       {cfg.Password},
			oauthKeyClientID: {cfg.ClientID},
		}
	} else {
		form = url.Values{
			"grant_type":         {"client_credentials"},
			oauthKeyClientID:     {cfg.ClientID},
			oauthKeyClientSecret: {cfg.ClientSecret},
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "failed to create token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "failed to execute token request")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "failed to read token response")
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", time.Time{}, errors.Wrap(err, "failed to parse token response")
	}

	if tr.Error != "" {
		return "", time.Time{}, errors.Errorf("token request failed: %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, errors.New("token response contained no access_token")
	}

	// Calculate expiry as now + ExpiresIn - 10 second buffer to refresh before expiration
	exp := time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second - 10*time.Second)
	return tr.AccessToken, exp, nil
}

// refreshToken checks if the access token is expired and fetches a new one if necessary.
// If no config is available (e.g. in tests), it skips refresh.
//
// While the most recent token fetch failed and the backoff window has not
// elapsed, refreshToken returns the cached failure wrapped in
// ErrAuthUnavailable so callers can apply backpressure (e.g. controller
// RequeueAfter) without hammering the upstream token endpoint.
func (k *keycloakClient) refreshToken(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if time.Now().Before(k.tokenExp) {
		return nil // token still valid
	}
	if k.cfg == nil {
		return nil // no config available, skip refresh (e.g. in tests)
	}

	// If we are still inside the backoff window after a recent failure,
	// short-circuit and surface the cached error so requesters can apply
	// backpressure instead of issuing another token request.
	now := time.Now()
	if k.lastFailure != nil && now.Before(k.backoffUntil) {
		return errors.Wrapf(ErrAuthUnavailable, "token fetch in backoff until %s (last error: %v)", k.backoffUntil.Format(time.RFC3339), k.lastFailure)
	}

	token, exp, err := fetchOAuth2Token(ctx, k.httpClient, k.baseURL, k.cfg)
	if err != nil {
		k.recordFailureLocked(err, now)
		return errors.Wrapf(ErrAuthUnavailable, "cannot obtain access token: %v", err)
	}
	k.recordSuccessLocked(token, exp, now)
	return nil
}

// recordFailureLocked updates the failure/backoff state. Caller must hold k.mu.
func (k *keycloakClient) recordFailureLocked(err error, now time.Time) {
	k.lastFailure = err
	k.lastFailureAt = now
	k.consecutiveFails++
	delay := backoffInitial << (k.consecutiveFails - 1)
	if delay <= 0 || delay > backoffMax {
		delay = backoffMax
	}
	k.backoffUntil = now.Add(delay)
}

// recordSuccessLocked clears failure state and stores the new token. Caller must hold k.mu.
func (k *keycloakClient) recordSuccessLocked(token string, exp time.Time, _ time.Time) {
	k.token = token
	k.tokenExp = exp
	k.lastFailure = nil
	k.lastFailureAt = time.Time{}
	k.backoffUntil = time.Time{}
	k.consecutiveFails = 0
}

// parseRetryAfter parses a Retry-After header value, returning the duration
// in seconds. It handles both HTTP-date (RFC 7231) and delta-seconds formats.
func parseRetryAfter(hdr string) time.Duration {
	if hdr == "" {
		return 0
	}
	if d, err := strconv.Atoi(hdr); err == nil && d > 0 {
		return time.Duration(d) * time.Second
	}
	if t, err := http.ParseTime(hdr); err == nil {
		if wait := time.Until(t); wait > 0 {
			return wait
		}
	}
	return 0
}

// checkRateLimitBackoff returns ErrRateLimited if we are currently in a rate
// limit backoff window, along with the remaining duration. Callers should
// treat this like ErrAuthUnavailable and apply RequeueAfter.
func (k *keycloakClient) checkRateLimitBackoff() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Now().Before(k.rateLimitUntil) {
		wait := time.Until(k.rateLimitUntil)
		return &RateLimitError{
			deadline: k.rateLimitUntil,
			wait:     wait,
		}
	}
	return nil
}

// recordRateLimitHit records a 429 response. If a Retry-After header was
// present, use that value as the backoff (capped at rateLimitBackoffMax).
// Otherwise, use exponential backoff starting at rateLimitBackoffInitial and
// doubling on each consecutive hit, up to rateLimitBackoffMax.
// A small random jitter is added to the deadline to prevent thundering herd
// when multiple reconciliation cycles all retry at exactly the same instant
// the backoff window expires.
func (k *keycloakClient) recordRateLimitHit(retryAfter time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := time.Now()
	if retryAfter > 0 {
		if retryAfter > rateLimitBackoffMax {
			retryAfter = rateLimitBackoffMax
		}
		k.rateLimitUntil = now.Add(retryAfter).Add(rateLimitBackoffJitter)
		k.rateLimitConsecutive++
		return
	}
	k.rateLimitConsecutive++
	delay := rateLimitBackoffInitial << (k.rateLimitConsecutive - 1)
	if delay <= 0 || delay > rateLimitBackoffMax {
		delay = rateLimitBackoffMax
	}
	k.rateLimitUntil = now.Add(delay).Add(rateLimitBackoffJitter)
}

// clearRateLimitBackoff clears the rate limit backoff state on success.
func (k *keycloakClient) clearRateLimitBackoff() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.rateLimitUntil = time.Time{}
	k.rateLimitConsecutive = 0
}

// =============================================================================
// HTTP Methods
// =============================================================================

func (c *keycloakClient) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	if err := c.refreshToken(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to refresh access token")
	}

	if err := c.checkRateLimitBackoff(); err != nil {
		return nil, err
	}

	// Acquire semaphore slot to limit concurrent requests. This prevents
	// thundering herd on 429 errors where all N concurrent workers slip
	// through the backoff check and hit the server simultaneously.
	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "request cancelled while waiting for concurrency slot")
	case c.requestSemaphore <- struct{}{}:
		defer func() { <-c.requestSemaphore }()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal request body")
		}
		if debugHTTP && method == http.MethodPost && path == adminPath {
			redacted := redactSensitiveJSON(bodyBytes)
			fmt.Printf("DEBUGHTTP body (len=%d, sha256=%x, valid-json=%v): %s\n",
				len(bodyBytes), sha256.Sum256(bodyBytes), json.Valid(bodyBytes), redacted)
			counted := &countingReader{r: bytes.NewReader(bodyBytes)}
			defer func() {
				fmt.Printf("DEBUGHTTP bytes actually read from body by transport: %d of %d\n", counted.n, len(bodyBytes))
			}()
			bodyReader = counted
		} else {
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	if counted, ok := bodyReader.(*countingReader); ok {
		// io.Reader (unlike *bytes.Reader) isn't special-cased by
		// NewRequestWithContext for Content-Length auto-detection -
		// restore identical wire behavior to the non-debug path.
		req.ContentLength = counted.r.Size()
	}

	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	if debugHTTP {
		safeURL := strings.ReplaceAll(req.URL.String(), "\n", "")
		safeURL = strings.ReplaceAll(safeURL, "\r", "")
		fmt.Printf("DEBUGHTTP request: method=%s url=%s content-length=%d transfer-encoding=%v proto=%s host=%s\n",
			req.Method, safeURL, req.ContentLength, req.TransferEncoding, req.Proto, req.Host)
		if method == http.MethodPost && path == adminPath {
			// Dump the exact wire bytes net/http would send, including any
			// headers the Transport itself adds. Built from a throwaway
			// request sharing the same method/URL/headers/body so the real
			// req and its body reader (possibly the instrumented
			// countingReader above) are never touched by this dump.
			//
			// The dump carries credentials, so both halves are redacted: the
			// body is marshalled and scrubbed rather than reused raw, and the
			// headers have their Authorization replaced. Dumping either
			// verbatim would write the bearer token and any password in the
			// payload to the pod log.
			bodyBytes, _ := json.Marshal(body)
			dumpReq, dErr := http.NewRequest(method, c.baseURL+path, bytes.NewReader(redactSensitiveJSON(bodyBytes)))
			if dErr == nil {
				dumpReq.Header = redactHeaders(req.Header)
				dump, dumpErr := httputil.DumpRequestOut(dumpReq, true)
				if dumpErr != nil {
					fmt.Printf("DEBUGHTTP dump failed: %v\n", dumpErr)
				} else {
					fmt.Printf("DEBUGHTTP wire dump (%d bytes):\n%s\n", len(dump), string(dump))
				}
			}
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute request")
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	if debugHTTP {
		// Redacted for the same reason as the request dump: the response is a
		// full realm, user or client representation, and printing it verbatim
		// puts whatever credentials it carries into the pod log. The header
		// copy matters as well - a response can echo a Set-Cookie.
		fmt.Printf("DEBUGHTTP response: status=%d proto=%s content-length=%d transfer-encoding=%v headers=%v body=%q\n",
			resp.StatusCode, resp.Proto, resp.ContentLength, resp.TransferEncoding,
			redactHeaders(resp.Header), string(redactSensitiveJSON(respBody)))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			c.recordRateLimitHit(retryAfter)
			// Return the RateLimitError so callers (especially Crossplane reconcilers)
			// can detect it and respect the specific backoff deadline.
			c.mu.Lock()
			deadline := c.rateLimitUntil
			c.mu.Unlock()
			wait := time.Until(deadline)
			return nil, &RateLimitError{deadline: deadline, wait: wait}
		}
		msg := string(respBody)
		if len(msg) > maxErrBodyLen {
			msg = msg[:maxErrBodyLen] + "..."
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, &notFoundError{status: resp.StatusCode, body: msg}
		}
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
	}

	c.clearRateLimitBackoff()
	return respBody, nil
}

// =============================================================================
// Realm Operations
// =============================================================================

type Realm struct {
	Realm                               string            `json:"realm"`
	Enabled                             bool              `json:"enabled"`
	DisplayName                         string            `json:"displayName,omitempty"`
	DisplayNameHtml                     string            `json:"displayNameHtml,omitempty"`
	SslRequired                         string            `json:"sslRequired,omitempty"`
	RegistrationAllowed                 bool              `json:"registrationAllowed"`
	RegistrationEmailAsUsername         bool              `json:"registrationEmailAsUsername"`
	EditUsernameAllowed                 bool              `json:"editUsernameAllowed"`
	ResetPasswordAllowed                bool              `json:"resetPasswordAllowed"`
	RememberMe                          bool              `json:"rememberMe"`
	VerifyEmail                         bool              `json:"verifyEmail"`
	LoginWithEmailAllowed               bool              `json:"loginWithEmailAllowed"`
	DuplicateEmailsAllowed              bool              `json:"duplicateEmailsAllowed"`
	DefaultSignatureAlgorithm           string            `json:"defaultSignatureAlgorithm,omitempty"`
	RevokeRefreshToken                  bool              `json:"revokeRefreshToken"`
	RefreshTokenMaxReuse                int64             `json:"refreshTokenMaxReuse"`
	AccessTokenLifespan                 interface{}       `json:"accessTokenLifespan,omitempty"`
	AccessTokenLifespanForImplicitFlow  interface{}       `json:"accessTokenLifespanForImplicitFlow,omitempty"`
	SsoSessionIdleTimeout               interface{}       `json:"ssoSessionIdleTimeout,omitempty"`
	SsoSessionMaxLifespan               interface{}       `json:"ssoSessionMaxLifespan,omitempty"`
	SsoSessionIdleTimeoutRememberMe     interface{}       `json:"ssoSessionIdleTimeoutRememberMe,omitempty"`
	SsoSessionMaxLifespanRememberMe     interface{}       `json:"ssoSessionMaxLifespanRememberMe,omitempty"`
	OfflineSessionIdleTimeout           interface{}       `json:"offlineSessionIdleTimeout,omitempty"`
	OfflineSessionMaxLifespanEnabled    bool              `json:"offlineSessionMaxLifespanEnabled"`
	OfflineSessionMaxLifespan           interface{}       `json:"offlineSessionMaxLifespan,omitempty"`
	ClientSessionIdleTimeout            interface{}       `json:"clientSessionIdleTimeout,omitempty"`
	ClientSessionMaxLifespan            interface{}       `json:"clientSessionMaxLifespan,omitempty"`
	AccessCodeLifespan                  interface{}       `json:"accessCodeLifespan,omitempty"`
	AccessCodeLifespanUserAction        interface{}       `json:"accessCodeLifespanUserAction,omitempty"`
	AccessCodeLifespanLogin             interface{}       `json:"accessCodeLifespanLogin,omitempty"`
	ActionTokenGeneratedByAdminLifespan interface{}       `json:"actionTokenGeneratedByAdminLifespan,omitempty"`
	ActionTokenGeneratedByUserLifespan  interface{}       `json:"actionTokenGeneratedByUserLifespan,omitempty"`
	BruteForceProtected                 bool              `json:"bruteForceProtected"`
	PasswordPolicy                      string            `json:"passwordPolicy,omitempty"`
	LoginTheme                          string            `json:"loginTheme,omitempty"`
	AccountTheme                        string            `json:"accountTheme,omitempty"`
	AdminTheme                          string            `json:"adminTheme,omitempty"`
	EmailTheme                          string            `json:"emailTheme,omitempty"`
	DefaultDefaultClientScopes          []string          `json:"defaultDefaultClientScopes,omitempty"`
	DefaultOptionalClientScopes         []string          `json:"defaultOptionalClientScopes,omitempty"`
	BrowserFlow                         string            `json:"browserFlow,omitempty"`
	RegistrationFlow                    string            `json:"registrationFlow,omitempty"`
	DirectGrantFlow                     string            `json:"directGrantFlow,omitempty"`
	ResetCredentialsFlow                string            `json:"resetCredentialsFlow,omitempty"`
	ClientAuthenticationFlow            string            `json:"clientAuthenticationFlow,omitempty"`
	UserManagedAccess                   bool              `json:"userManagedAccessAllowed"`
	AdminPermissionsEnabled             bool              `json:"adminPermissionsEnabled"`
	FrontendURL                         *string           `json:"frontendUrl,omitempty"`
	Attributes                          map[string]string `json:"attributes,omitempty"`
	// SmtpServer is Keycloak's RealmRepresentation.smtpServer map[string]string.
	// Keycloak 26.x rejects any nested-object form ("Cannot parse the JSON");
	// the only accepted shape is the flat string->string map.
	SmtpServer map[string]string `json:"smtpServer,omitempty"`
}

// DurationToSeconds converts a Go duration string (e.g. "30m0s") to integer seconds.
// Returns 0 if the input is empty.
func DurationToSeconds(d string) int {
	if d == "" {
		return 0
	}
	parsed, err := time.ParseDuration(d)
	if err != nil {
		return 0
	}
	return int(parsed.Seconds())
}

// SecondsToDuration converts an integer seconds value to a Go duration string.
// Handles both int and float64 from JSON unmarshalling.
func SecondsToDuration(v interface{}) string {
	switch val := v.(type) {
	case float64:
		return (time.Duration(val) * time.Second).String()
	case int:
		return (time.Duration(val) * time.Second).String()
	case int64:
		return (time.Duration(val) * time.Second).String()
	case json.Number:
		n, _ := val.Int64()
		return (time.Duration(n) * time.Second).String()
	default:
		return ""
	}
}

func (c *keycloakClient) GetRealm(ctx context.Context, realm string) (*Realm, error) {
	respBody, err := c.doRequest(ctx, http.MethodGet, realmPath(realm), nil)
	if err != nil {
		return nil, err
	}

	var r Realm
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal realm")
	}

	return &r, nil
}

func (c *keycloakClient) CreateRealm(ctx context.Context, realm *Realm) (*Realm, error) {
	respBody, err := c.doRequest(ctx, http.MethodPost, adminPath, realm)
	if err != nil {
		return nil, err
	}

	var r Realm
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal realm")
	}

	return &r, nil
}

func (c *keycloakClient) UpdateRealm(ctx context.Context, realm *Realm) error {
	_, err := c.doRequest(ctx, http.MethodPut, realmPath(realm.Realm), realm)
	return err
}

// GetRawRealm returns the raw JSON bytes of a realm to preserve all fields
func (c *keycloakClient) GetRawRealm(ctx context.Context, realm string) ([]byte, error) {
	return c.doRequest(ctx, http.MethodGet, realmPath(realm), nil)
}

// UpdateRealmRaw sends raw JSON bytes to update a realm
func (c *keycloakClient) UpdateRealmRaw(ctx context.Context, realm string, realmJSON []byte) error {
	if err := c.refreshToken(ctx); err != nil {
		return errors.Wrap(err, "failed to refresh access token")
	}

	if err := c.checkRateLimitBackoff(); err != nil {
		return err
	}

	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+realmPath(realm), bytes.NewReader(realmJSON))
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to execute request")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			c.recordRateLimitHit(retryAfter)
			// Return the RateLimitError so callers (especially Crossplane reconcilers)
			// can detect it and respect the specific backoff deadline.
			c.mu.Lock()
			deadline := c.rateLimitUntil
			c.mu.Unlock()
			wait := time.Until(deadline)
			return &RateLimitError{deadline: deadline, wait: wait}
		}
		return errors.New(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)))
	}
	c.clearRateLimitBackoff()
	return nil
}

func (c *keycloakClient) DeleteRealm(ctx context.Context, realm string) error {
	_, err := c.doRequest(ctx, http.MethodDelete, realmPath(realm), nil)
	return err
}

// =============================================================================
// Client Operations
// =============================================================================

type ClientRepresentation struct {
	ID                                     string            `json:"id,omitempty"`
	ClientID                               string            `json:"clientId"`
	Name                                   string            `json:"name,omitempty"`
	Description                            string            `json:"description,omitempty"`
	Enabled                                bool              `json:"enabled"`
	RootURL                                string            `json:"rootUrl,omitempty"`
	HomeURL                                string            `json:"homeUrl,omitempty"`
	BaseURL                                string            `json:"baseUrl,omitempty"`
	AdminURL                               string            `json:"adminUrl,omitempty"`
	ValidRedirectURIs                      []string          `json:"redirectUris,omitempty"`
	WebOrigins                             []string          `json:"webOrigins,omitempty"`
	StandardFlowEnabled                    bool              `json:"standardFlowEnabled"`
	DirectAccessGrantsEnabled              bool              `json:"directAccessGrantsEnabled"`
	ImplicitFlowEnabled                    bool              `json:"implicitFlowEnabled"`
	ServiceAccountsEnabled                 bool              `json:"serviceAccountsEnabled"`
	PublicClient                           bool              `json:"publicClient"`
	BearerOnly                             bool              `json:"bearerOnly"`
	ConsentRequired                        bool              `json:"consentRequired"`
	FullScopeAllowed                       bool              `json:"fullScopeAllowed"`
	AlwaysDisplayInConsole                 bool              `json:"alwaysDisplayInConsole"`
	FrontchannelLogoutEnabled              *bool             `json:"frontchannelLogout,omitempty"`
	FrontchannelLogoutURL                  *string           `json:"frontchannelLogoutUrl,omitempty"`
	BackchannelLogoutURL                   string            `json:"backchannelLogoutUrl,omitempty"`
	BackchannelLogoutSessionRequired       *bool             `json:"backchannelLogoutSessionRequired,omitempty"`
	BackchannelLogoutRevokeOfflineSessions *bool             `json:"backchannelLogoutRevokeOfflineSessions,omitempty"`
	Protocol                               string            `json:"protocol,omitempty"`
	AuthorizationServicesEnabled           *bool             `json:"authorizationServicesEnabled,omitempty"`
	OAuth2DeviceAuthorizationGrantEnabled  *bool             `json:"oauth2DeviceAuthorizationGrantEnabled,omitempty"`
	StandardTokenExchangeEnabled           *bool             `json:"standardTokenExchangeEnabled,omitempty"`
	UseRefreshTokens                       *bool             `json:"useRefreshTokens,omitempty"`
	ClientSessionIdleTimeout               string            `json:"clientSessionIdleTimeout,omitempty"`
	ClientSessionMaxLifespan               string            `json:"clientSessionMaxLifespan,omitempty"`
	ClientOfflineSessionIdleTimeout        string            `json:"clientOfflineSessionIdleTimeout,omitempty"`
	ClientOfflineSessionMaxLifespan        string            `json:"clientOfflineSessionMaxLifespan,omitempty"`
	PkceCodeChallengeMethod                string            `json:"pkceCodeChallengeMethod,omitempty"`
	Attributes                             map[string]string `json:"attributes,omitempty"`
	Secret                                 string            `json:"secret,omitempty"`
}

// fieldsRoutedViaAttributes lists fields that the Keycloak 26.x Admin REST
// API rejects as "Unrecognized field" when sent at the top level of the
// PUT /admin/realms/{realm}/clients/{id} body, but which it DOES accept
// when placed under attributes.<dot.notation> in the same body. Verified
// 2026-07-30 against quay.io/keycloak/keycloak:26.x.
//
// The struct keeps these as top-level fields so existing controller code
// (drift comparison, parameter mapping) does not need to change; the
// custom MarshalJSON/UnmarshalJSON below translate between the two
// representations transparently.
//
// homeUrl is also accepted via attributes.home.page.url but the Keycloak
// representation appears to round-trip homeUrl as itself in some versions,
// so it is left as a top-level field rather than routed via attributes.
var fieldsRoutedViaAttributes = []struct {
	attrKey   string
	boolPtr   *bool
	stringPtr *string
	strRef    *string
}{
	{"frontchannel.logout.url", nil, nil, nil}, // below uses ptr
	{"backchannel.logout.url", nil, nil, nil},
	{"backchannel.logout.session.required", nil, nil, nil},
	{"backchannel.logout.revoke.offline.sessions", nil, nil, nil},
	{"oauth2.device.authorization.grant.enabled", nil, nil, nil},
	{"standard.token.exchange.enabled", nil, nil, nil},
	{"use.refresh.tokens", nil, nil, nil},
	{"client.session.idle.timeout", nil, nil, nil},
	{"client.session.max.lifespan", nil, nil, nil},
	{"client.offline.session.idle.timeout", nil, nil, nil},
	{"client.offline.session.max.lifespan", nil, nil, nil},
}

// MarshalJSON serialises ClientRepresentation to the wire format Keycloak 26.x
// accepts. Fields that Keycloak rejects at the top level are moved into the
// attributes map under their dot-notation names so the provider remains the
// source of truth for them via the CR spec.
func (c *ClientRepresentation) MarshalJSON() ([]byte, error) {
	// Use a separate (non-recursive) struct to avoid triggering our own
	// MarshalJSON. clientWire mirrors ClientRepresentation exactly; if a
	// field is added to one it must be added to the other.
	type clientWire = struct {
		ID                                     string            `json:"id,omitempty"`
		ClientID                               string            `json:"clientId"`
		Name                                   string            `json:"name,omitempty"`
		Description                            string            `json:"description,omitempty"`
		Enabled                                bool              `json:"enabled"`
		RootURL                                string            `json:"rootUrl,omitempty"`
		HomeURL                                string            `json:"homeUrl,omitempty"`
		BaseURL                                string            `json:"baseUrl,omitempty"`
		AdminURL                               string            `json:"adminUrl,omitempty"`
		ValidRedirectURIs                      []string          `json:"redirectUris,omitempty"`
		WebOrigins                             []string          `json:"webOrigins,omitempty"`
		StandardFlowEnabled                    bool              `json:"standardFlowEnabled"`
		DirectAccessGrantsEnabled              bool              `json:"directAccessGrantsEnabled"`
		ImplicitFlowEnabled                    bool              `json:"implicitFlowEnabled"`
		ServiceAccountsEnabled                 bool              `json:"serviceAccountsEnabled"`
		PublicClient                           bool              `json:"publicClient"`
		BearerOnly                             bool              `json:"bearerOnly"`
		ConsentRequired                        bool              `json:"consentRequired"`
		FullScopeAllowed                       bool              `json:"fullScopeAllowed"`
		AlwaysDisplayInConsole                 bool              `json:"alwaysDisplayInConsole"`
		FrontchannelLogoutEnabled              *bool             `json:"frontchannelLogout,omitempty"`
		FrontchannelLogoutURL                  *string           `json:"frontchannelLogoutUrl,omitempty"`
		BackchannelLogoutURL                   string            `json:"backchannelLogoutUrl,omitempty"`
		BackchannelLogoutSessionRequired       *bool             `json:"backchannelLogoutSessionRequired,omitempty"`
		BackchannelLogoutRevokeOfflineSessions *bool             `json:"backchannelLogoutRevokeOfflineSessions,omitempty"`
		Protocol                               string            `json:"protocol,omitempty"`
		AuthorizationServicesEnabled           *bool             `json:"authorizationServicesEnabled,omitempty"`
		OAuth2DeviceAuthorizationGrantEnabled  *bool             `json:"oauth2DeviceAuthorizationGrantEnabled,omitempty"`
		StandardTokenExchangeEnabled           *bool             `json:"standardTokenExchangeEnabled,omitempty"`
		UseRefreshTokens                       *bool             `json:"useRefreshTokens,omitempty"`
		ClientSessionIdleTimeout               string            `json:"clientSessionIdleTimeout,omitempty"`
		ClientSessionMaxLifespan               string            `json:"clientSessionMaxLifespan,omitempty"`
		ClientOfflineSessionIdleTimeout        string            `json:"clientOfflineSessionIdleTimeout,omitempty"`
		ClientOfflineSessionMaxLifespan        string            `json:"clientOfflineSessionMaxLifespan,omitempty"`
		PkceCodeChallengeMethod                string            `json:"pkceCodeChallengeMethod,omitempty"`
		Attributes                             map[string]string `json:"attributes,omitempty"`
		Secret                                 string            `json:"secret,omitempty"`
	}
	tmp, err := json.Marshal(clientWire{
		ID:                                     c.ID,
		ClientID:                               c.ClientID,
		Name:                                   c.Name,
		Description:                            c.Description,
		Enabled:                                c.Enabled,
		RootURL:                                c.RootURL,
		HomeURL:                                c.HomeURL,
		BaseURL:                                c.BaseURL,
		AdminURL:                               c.AdminURL,
		ValidRedirectURIs:                      c.ValidRedirectURIs,
		WebOrigins:                             c.WebOrigins,
		StandardFlowEnabled:                    c.StandardFlowEnabled,
		DirectAccessGrantsEnabled:              c.DirectAccessGrantsEnabled,
		ImplicitFlowEnabled:                    c.ImplicitFlowEnabled,
		ServiceAccountsEnabled:                 c.ServiceAccountsEnabled,
		PublicClient:                           c.PublicClient,
		BearerOnly:                             c.BearerOnly,
		ConsentRequired:                        c.ConsentRequired,
		FullScopeAllowed:                       c.FullScopeAllowed,
		AlwaysDisplayInConsole:                 c.AlwaysDisplayInConsole,
		FrontchannelLogoutEnabled:              c.FrontchannelLogoutEnabled,
		FrontchannelLogoutURL:                  c.FrontchannelLogoutURL,
		BackchannelLogoutURL:                   c.BackchannelLogoutURL,
		BackchannelLogoutSessionRequired:       c.BackchannelLogoutSessionRequired,
		BackchannelLogoutRevokeOfflineSessions: c.BackchannelLogoutRevokeOfflineSessions,
		Protocol:                               c.Protocol,
		AuthorizationServicesEnabled:           c.AuthorizationServicesEnabled,
		OAuth2DeviceAuthorizationGrantEnabled:  c.OAuth2DeviceAuthorizationGrantEnabled,
		StandardTokenExchangeEnabled:           c.StandardTokenExchangeEnabled,
		UseRefreshTokens:                       c.UseRefreshTokens,
		ClientSessionIdleTimeout:               c.ClientSessionIdleTimeout,
		ClientSessionMaxLifespan:               c.ClientSessionMaxLifespan,
		ClientOfflineSessionIdleTimeout:        c.ClientOfflineSessionIdleTimeout,
		ClientOfflineSessionMaxLifespan:        c.ClientOfflineSessionMaxLifespan,
		PkceCodeChallengeMethod:                c.PkceCodeChallengeMethod,
		Attributes:                             c.Attributes,
		Secret:                                 c.Secret,
	})
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(tmp, &out); err != nil {
		return nil, err
	}
	attrs := map[string]string{}
	if a, ok := out["attributes"]; ok {
		if err := json.Unmarshal(a, &attrs); err != nil {
			return nil, err
		}
	}
	// Move routed fields into attributes.<dot.notation> and drop them from
	// the top-level output. Each block follows the same pattern: pull the
	// current value, drop the top-level key, write into attrs.
	if c.HomeURL != "" {
		attrs["home.page.url"] = c.HomeURL
		delete(out, "homeUrl")
	}
	if c.FrontchannelLogoutURL != nil && *c.FrontchannelLogoutURL != "" {
		attrs["frontchannel.logout.url"] = *c.FrontchannelLogoutURL
		delete(out, "frontchannelLogoutUrl")
	}
	if c.BackchannelLogoutURL != "" {
		attrs["backchannel.logout.url"] = c.BackchannelLogoutURL
		delete(out, "backchannelLogoutUrl")
	}
	if c.BackchannelLogoutSessionRequired != nil {
		attrs["backchannel.logout.session.required"] = strconv.FormatBool(*c.BackchannelLogoutSessionRequired)
		delete(out, "backchannelLogoutSessionRequired")
	}
	if c.BackchannelLogoutRevokeOfflineSessions != nil {
		attrs["backchannel.logout.revoke.offline.sessions"] = strconv.FormatBool(*c.BackchannelLogoutRevokeOfflineSessions)
		delete(out, "backchannelLogoutRevokeOfflineSessions")
	}
	if c.OAuth2DeviceAuthorizationGrantEnabled != nil {
		attrs["oauth2.device.authorization.grant.enabled"] = strconv.FormatBool(*c.OAuth2DeviceAuthorizationGrantEnabled)
		delete(out, "oauth2DeviceAuthorizationGrantEnabled")
	}
	if c.StandardTokenExchangeEnabled != nil {
		attrs["standard.token.exchange.enabled"] = strconv.FormatBool(*c.StandardTokenExchangeEnabled)
		delete(out, "standardTokenExchangeEnabled")
	}
	if c.UseRefreshTokens != nil {
		attrs["use.refresh.tokens"] = strconv.FormatBool(*c.UseRefreshTokens)
		delete(out, "useRefreshTokens")
	}
	if c.ClientSessionIdleTimeout != "" {
		attrs["client.session.idle.timeout"] = c.ClientSessionIdleTimeout
		delete(out, "clientSessionIdleTimeout")
	}
	if c.ClientSessionMaxLifespan != "" {
		attrs["client.session.max.lifespan"] = c.ClientSessionMaxLifespan
		delete(out, "clientSessionMaxLifespan")
	}
	if c.ClientOfflineSessionIdleTimeout != "" {
		attrs["client.offline.session.idle.timeout"] = c.ClientOfflineSessionIdleTimeout
		delete(out, "clientOfflineSessionIdleTimeout")
	}
	if c.ClientOfflineSessionMaxLifespan != "" {
		attrs["client.offline.session.max.lifespan"] = c.ClientOfflineSessionMaxLifespan
		delete(out, "clientOfflineSessionMaxLifespan")
	}
	_ = fieldsRoutedViaAttributes // documentation reference
	if len(attrs) > 0 {
		ab, err := json.Marshal(attrs)
		if err != nil {
			return nil, err
		}
		out["attributes"] = ab
	} else {
		delete(out, "attributes")
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses Keycloak 26.x GET responses. Top-level fields are
// unmarshalled normally, then dot-notation entries under attributes are
// promoted back to the typed struct fields so the controller's drift
// comparison sees the actual Keycloak state instead of zero values.
func (c *ClientRepresentation) UnmarshalJSON(data []byte) error {
	// Avoid infinite recursion by marshalling into a local alias.
	type alias ClientRepresentation
	aux := (*alias)(c)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if c.Attributes == nil {
		return nil
	}
	if v, ok := c.Attributes["home.page.url"]; ok {
		c.HomeURL = v
	}
	if v, ok := c.Attributes["frontchannel.logout.url"]; ok {
		s := v
		c.FrontchannelLogoutURL = &s
	}
	if v, ok := c.Attributes["backchannel.logout.url"]; ok {
		c.BackchannelLogoutURL = v
	}
	if v, ok := c.Attributes["backchannel.logout.session.required"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.BackchannelLogoutSessionRequired = &b
		}
	}
	if v, ok := c.Attributes["backchannel.logout.revoke.offline.sessions"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.BackchannelLogoutRevokeOfflineSessions = &b
		}
	}
	if v, ok := c.Attributes["oauth2.device.authorization.grant.enabled"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.OAuth2DeviceAuthorizationGrantEnabled = &b
		}
	}
	if v, ok := c.Attributes["standard.token.exchange.enabled"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.StandardTokenExchangeEnabled = &b
		}
	}
	if v, ok := c.Attributes["use.refresh.tokens"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.UseRefreshTokens = &b
		}
	}
	if v, ok := c.Attributes["client.session.idle.timeout"]; ok {
		c.ClientSessionIdleTimeout = v
	}
	if v, ok := c.Attributes["client.session.max.lifespan"]; ok {
		c.ClientSessionMaxLifespan = v
	}
	if v, ok := c.Attributes["client.offline.session.idle.timeout"]; ok {
		c.ClientOfflineSessionIdleTimeout = v
	}
	if v, ok := c.Attributes["client.offline.session.max.lifespan"]; ok {
		c.ClientOfflineSessionMaxLifespan = v
	}
	return nil
}

func (c *keycloakClient) GetClient(ctx context.Context, realm, clientID string) (*ClientRepresentation, error) {
	// If v2 is enabled, try v2 first with fallback to v1 if not available
	if c.cfg != nil && c.cfg.AdminAPIv2Enabled {
		client, err := c.getClientV2(ctx, realm, clientID)
		if err == nil || !errors.Is(err, ErrNotFound) {
			// Either found (nil error) or a non-404 error, return as-is
			return client, err
		}
		// If v2 is not available (404), silently fall back to v1
		return c.getClientV1(ctx, realm, clientID)
	}
	// Default to v1
	return c.getClientV1(ctx, realm, clientID)
}

// getClientV1 uses the Admin API v1 endpoint.
func (c *keycloakClient) getClientV1(ctx context.Context, realm, clientID string) (*ClientRepresentation, error) {
	path := realmPath(realm) + "/clients?clientId=" + url.QueryEscape(clientID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var clients []ClientRepresentation
	if err := json.Unmarshal(respBody, &clients); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal clients")
	}

	if len(clients) == 0 {
		return nil, nil
	}

	return &clients[0], nil
}

// getClientV2 uses the Admin API v2 endpoint (Keycloak 26.8+) with query filtering.
// Experimental: requires --features=client-admin-api-v2 on the server.
func (c *keycloakClient) getClientV2(ctx context.Context, realm, clientID string) (*ClientRepresentation, error) {
	// v2 API supports query expressions: clientId=<value>
	// URL encode the clientID to safely embed it in the query expression
	queryExpr := "clientId=" + url.QueryEscape(clientID)
	path := adminAPIv2Path + "/" + url.PathEscape(realm) + "/clients/v2?q=" + url.QueryEscape(queryExpr)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var clients []ClientRepresentation
	if err := json.Unmarshal(respBody, &clients); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal clients from v2 API")
	}

	if len(clients) == 0 {
		return nil, nil
	}

	return &clients[0], nil
}

func (c *keycloakClient) CreateClient(ctx context.Context, realm string, client *ClientRepresentation) (*ClientRepresentation, error) {
	// Keycloak POST /clients returns HTTP 201 with an empty body.
	// The internal UUID is in the Location header's last path segment.
	id, err := c.doCreate(ctx, realmPath(realm)+"/clients", client)
	if err != nil {
		return nil, err
	}
	created := *client
	created.ID = id
	return &created, nil
}

// doCreate POSTs body to path and extracts the created resource UUID from the
// Location response header.  Keycloak returns Location: .../clients/{uuid}.
func (c *keycloakClient) doCreate(ctx context.Context, path string, body interface{}) (string, error) {
	if err := c.refreshToken(ctx); err != nil {
		return "", errors.Wrap(err, "failed to refresh access token")
	}

	if err := c.checkRateLimitBackoff(); err != nil {
		return "", err
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal request body")
	}

	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to execute request")
	}
	defer func() { _ = resp.Body.Close() }()

	// Read and discard body to allow connection reuse.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyLen+1))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			c.recordRateLimitHit(retryAfter)
			// Return the RateLimitError so callers (especially Crossplane reconcilers)
			// can detect it and respect the specific backoff deadline.
			c.mu.Lock()
			deadline := c.rateLimitUntil
			c.mu.Unlock()
			wait := time.Until(deadline)
			return "", &RateLimitError{deadline: deadline, wait: wait}
		}
		msg := string(respBody)
		if len(msg) > maxErrBodyLen {
			msg = msg[:maxErrBodyLen] + "..."
		}
		return "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
	}

	c.clearRateLimitBackoff()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", nil
	}
	// Location is .../clients/{uuid} — UUID is the last path segment.
	parsed, err := url.Parse(loc)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse Location header")
	}
	segments := strings.Split(strings.TrimRight(parsed.Path, "/"), "/")
	return segments[len(segments)-1], nil
}

func (c *keycloakClient) UpdateClient(ctx context.Context, realm string, client *ClientRepresentation) error {
	if client.ID == "" {
		return errors.New("client ID is required for update")
	}
	path := realmPath(realm) + "/clients/" + url.PathEscape(client.ID)
	_, err := c.doRequest(ctx, http.MethodPut, path, client)
	return err
}

func (c *keycloakClient) DeleteClient(ctx context.Context, realm, clientID string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListClients(ctx context.Context, realm string) ([]ClientRepresentation, error) {
	// If v2 is enabled, try v2 first with fallback to v1 if not available
	if c.cfg != nil && c.cfg.AdminAPIv2Enabled {
		clients, err := c.listClientsV2(ctx, realm)
		if err == nil {
			return clients, nil
		}
		// If v2 is not available (404), silently fall back to v1
		if errors.Is(err, ErrNotFound) {
			return c.listClientsV1(ctx, realm)
		}
		// For other errors, return them (e.g., authentication, rate limit)
		return nil, err
	}
	// Default to v1
	return c.listClientsV1(ctx, realm)
}

// listClientsV1 uses the Admin API v1 endpoint.
func (c *keycloakClient) listClientsV1(ctx context.Context, realm string) ([]ClientRepresentation, error) {
	path := realmPath(realm) + "/clients"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var clients []ClientRepresentation
	if err := json.Unmarshal(respBody, &clients); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal clients")
	}

	return clients, nil
}

// listClientsV2 uses the Admin API v2 endpoint (Keycloak 26.8+).
// Experimental: requires --features=client-admin-api-v2 on the server.
func (c *keycloakClient) listClientsV2(ctx context.Context, realm string) ([]ClientRepresentation, error) {
	path := adminAPIv2Path + "/" + url.PathEscape(realm) + "/clients/v2"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var clients []ClientRepresentation
	if err := json.Unmarshal(respBody, &clients); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal clients from v2 API")
	}

	return clients, nil
}

// =============================================================================
// User Operations
// =============================================================================

type UserRepresentation struct {
	ID            string              `json:"id,omitempty"`
	Username      string              `json:"username"`
	Email         string              `json:"email,omitempty"`
	FirstName     string              `json:"firstName,omitempty"`
	LastName      string              `json:"lastName,omitempty"`
	Enabled       bool                `json:"enabled"`
	EmailVerified bool                `json:"emailVerified"`
	Groups        []string            `json:"groups,omitempty"`
	RealmRoles    []string            `json:"realmRoles,omitempty"`
	ClientRoles   map[string][]string `json:"clientRoles,omitempty"`
	Attributes    map[string][]string `json:"attributes,omitempty"`
}

func (c *keycloakClient) GetUser(ctx context.Context, realm, username string) (*UserRepresentation, error) {
	path := realmPath(realm) + "/users?username=" + url.QueryEscape(username)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var users []UserRepresentation
	if err := json.Unmarshal(respBody, &users); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal users")
	}

	if len(users) == 0 {
		return nil, nil
	}

	return &users[0], nil
}

func (c *keycloakClient) CreateUser(ctx context.Context, realm string, user *UserRepresentation) (*UserRepresentation, error) {
	path := realmPath(realm) + "/users"
	respBody, err := c.doRequest(ctx, http.MethodPost, path, user)
	if err != nil {
		return nil, err
	}

	var created UserRepresentation
	if err := json.Unmarshal(respBody, &created); err != nil && len(respBody) > 0 {
		return nil, errors.Wrap(err, "failed to unmarshal created user")
	}

	return &created, nil
}

func (c *keycloakClient) UpdateUser(ctx context.Context, realm string, user *UserRepresentation) error {
	if user.ID == "" {
		return errors.New("user ID is required for update")
	}
	path := realmPath(realm) + "/users/" + url.PathEscape(user.ID)
	_, err := c.doRequest(ctx, http.MethodPut, path, user)
	return err
}

func (c *keycloakClient) DeleteUser(ctx context.Context, realm, userID string) error {
	path := realmPath(realm) + "/users/" + url.PathEscape(userID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListUsers(ctx context.Context, realm string) ([]UserRepresentation, error) {
	path := realmPath(realm) + "/users"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var users []UserRepresentation
	if err := json.Unmarshal(respBody, &users); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal users")
	}

	return users, nil
}

// =============================================================================
// Group Operations
// =============================================================================

type GroupRepresentation struct {
	ID          string              `json:"id,omitempty"`
	Name        string              `json:"name"`
	Path        string              `json:"path,omitempty"`
	RealmRoles  []string            `json:"realmRoles,omitempty"`
	ClientRoles map[string][]string `json:"clientRoles,omitempty"`
	Attributes  map[string]string   `json:"attributes,omitempty"`
}

func (c *keycloakClient) GetGroup(ctx context.Context, realm, groupID string) (*GroupRepresentation, error) {
	path := realmPath(realm) + "/groups/" + url.PathEscape(groupID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var g GroupRepresentation
	if err := json.Unmarshal(respBody, &g); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal group")
	}

	return &g, nil
}

func (c *keycloakClient) CreateGroup(ctx context.Context, realm string, group *GroupRepresentation) (*GroupRepresentation, error) {
	path := realmPath(realm) + "/groups"
	respBody, err := c.doRequest(ctx, http.MethodPost, path, group)
	if err != nil {
		return nil, err
	}

	var created GroupRepresentation
	if err := json.Unmarshal(respBody, &created); err != nil && len(respBody) > 0 {
		return nil, errors.Wrap(err, "failed to unmarshal created group")
	}

	return &created, nil
}

func (c *keycloakClient) UpdateGroup(ctx context.Context, realm string, group *GroupRepresentation) error {
	if group.ID == "" {
		return errors.New("group ID is required for update")
	}
	path := realmPath(realm) + "/groups/" + url.PathEscape(group.ID)
	_, err := c.doRequest(ctx, http.MethodPut, path, group)
	return err
}

func (c *keycloakClient) DeleteGroup(ctx context.Context, realm, groupID string) error {
	path := realmPath(realm) + "/groups/" + url.PathEscape(groupID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListGroups(ctx context.Context, realm string) ([]GroupRepresentation, error) {
	path := realmPath(realm) + "/groups"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var groups []GroupRepresentation
	if err := json.Unmarshal(respBody, &groups); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal groups")
	}

	return groups, nil
}

// =============================================================================
// Client Secret Operations
// =============================================================================

type clientSecretResponse struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (c *keycloakClient) GetClientSecret(ctx context.Context, realm, clientUUID string) (string, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/client-secret"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var s clientSecretResponse
	if err := json.Unmarshal(respBody, &s); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal client secret")
	}
	return s.Value, nil
}

// ResetClientSecret regenerates the client's secret.
//
// The Admin API exposes this as POST /clients/{uuid}/client-secret, taking no
// request body and returning the generated CredentialRepresentation. There is
// no PUT on that path, and no way to supply a specific value - the old
// implementation sent PUT with a body, which Keycloak rejects with 405.
func (c *keycloakClient) ResetClientSecret(ctx context.Context, realm, clientUUID string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/client-secret"
	_, err := c.doRequest(ctx, http.MethodPost, path, nil)
	return err
}

// =============================================================================
// Extended Group Operations
// =============================================================================

func (c *keycloakClient) SearchGroups(ctx context.Context, realm, name string) ([]GroupRepresentation, error) {
	path := realmPath(realm) + "/groups?search=" + url.QueryEscape(name)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var groups []GroupRepresentation
	if err := json.Unmarshal(respBody, &groups); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal groups")
	}
	return groups, nil
}

func (c *keycloakClient) GetUserGroups(ctx context.Context, realm, userUUID string) ([]GroupRepresentation, error) {
	path := realmPath(realm) + "/users/" + url.PathEscape(userUUID) + "/groups"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var groups []GroupRepresentation
	if err := json.Unmarshal(respBody, &groups); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal user groups")
	}
	return groups, nil
}

func (c *keycloakClient) AddUserToGroup(ctx context.Context, realm, userUUID, groupUUID string) error {
	path := realmPath(realm) + "/users/" + url.PathEscape(userUUID) + "/groups/" + url.PathEscape(groupUUID)
	_, err := c.doRequest(ctx, http.MethodPut, path, nil)
	return err
}

func (c *keycloakClient) RemoveUserFromGroup(ctx context.Context, realm, userUUID, groupUUID string) error {
	path := realmPath(realm) + "/users/" + url.PathEscape(userUUID) + "/groups/" + url.PathEscape(groupUUID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) SearchUsers(ctx context.Context, realm, username string) ([]UserRepresentation, error) {
	path := realmPath(realm) + "/users?username=" + url.QueryEscape(username) + "&exact=true"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var users []UserRepresentation
	if err := json.Unmarshal(respBody, &users); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal users")
	}
	return users, nil
}

// =============================================================================
// Role Operations
// =============================================================================

// RoleRepresentation is a Keycloak realm or client role.
type RoleRepresentation struct {
	ID          string              `json:"id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Composite   bool                `json:"composite,omitempty"`
	ClientRole  bool                `json:"clientRole,omitempty"`
	Attributes  map[string][]string `json:"attributes,omitempty"`
}

func (c *keycloakClient) GetRealmRole(ctx context.Context, realm, name string) (*RoleRepresentation, error) {
	path := realmPath(realm) + "/roles/" + url.PathEscape(name)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var r RoleRepresentation
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal role")
	}
	return &r, nil
}

func (c *keycloakClient) CreateRealmRole(ctx context.Context, realm string, role *RoleRepresentation) error {
	path := realmPath(realm) + "/roles"
	_, err := c.doRequest(ctx, http.MethodPost, path, role)
	return err
}

func (c *keycloakClient) UpdateRealmRole(ctx context.Context, realm, name string, role *RoleRepresentation) error {
	path := realmPath(realm) + "/roles/" + url.PathEscape(name)
	_, err := c.doRequest(ctx, http.MethodPut, path, role)
	return err
}

func (c *keycloakClient) DeleteRealmRole(ctx context.Context, realm, name string) error {
	path := realmPath(realm) + "/roles/" + url.PathEscape(name)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) GetClientRole(ctx context.Context, realm, clientUUID, name string) (*RoleRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/roles/" + url.PathEscape(name)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var r RoleRepresentation
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client role")
	}
	return &r, nil
}

func (c *keycloakClient) CreateClientRole(ctx context.Context, realm, clientUUID string, role *RoleRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/roles"
	_, err := c.doRequest(ctx, http.MethodPost, path, role)
	return err
}

func (c *keycloakClient) UpdateClientRole(ctx context.Context, realm, clientUUID, name string, role *RoleRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/roles/" + url.PathEscape(name)
	_, err := c.doRequest(ctx, http.MethodPut, path, role)
	return err
}

func (c *keycloakClient) DeleteClientRole(ctx context.Context, realm, clientUUID, name string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/roles/" + url.PathEscape(name)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// =============================================================================
// Protocol Mapper Operations
// =============================================================================

// ProtocolMapperRepresentation is a Keycloak protocol mapper.
type ProtocolMapperRepresentation struct {
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol"`
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config,omitempty"`
}

func (c *keycloakClient) ListClientProtocolMappers(ctx context.Context, realm, clientUUID string) ([]ProtocolMapperRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/protocol-mappers/models"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var mappers []ProtocolMapperRepresentation
	if err := json.Unmarshal(respBody, &mappers); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal protocol mappers")
	}
	return mappers, nil
}

func (c *keycloakClient) GetClientProtocolMapper(ctx context.Context, realm, clientUUID, mapperID string) (*ProtocolMapperRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/protocol-mappers/models/" + url.PathEscape(mapperID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var m ProtocolMapperRepresentation
	if err := json.Unmarshal(respBody, &m); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal protocol mapper")
	}
	return &m, nil
}

func (c *keycloakClient) CreateClientProtocolMapper(ctx context.Context, realm, clientUUID string, mapper *ProtocolMapperRepresentation) (string, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/protocol-mappers/models"
	return c.doCreate(ctx, path, mapper)
}

func (c *keycloakClient) UpdateClientProtocolMapper(ctx context.Context, realm, clientUUID string, mapper *ProtocolMapperRepresentation) error {
	if mapper.ID == "" {
		return errors.New("mapper ID is required for update")
	}
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/protocol-mappers/models/" + url.PathEscape(mapper.ID)
	_, err := c.doRequest(ctx, http.MethodPut, path, mapper)
	return err
}

func (c *keycloakClient) DeleteClientProtocolMapper(ctx context.Context, realm, clientUUID, mapperID string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/protocol-mappers/models/" + url.PathEscape(mapperID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// =============================================================================
// User Federation Operations
// =============================================================================

type UserFederationProviderRepresentation struct {
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name"`
	ProviderName string            `json:"providerName"`
	Priority     int32             `json:"priority,omitempty"`
	Config       map[string]string `json:"config,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
}

func (c *keycloakClient) ListUserFederationProviders(ctx context.Context, realm string) ([]UserFederationProviderRepresentation, error) {
	path := realmPath(realm) + "/user-federation/instances"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var providers []UserFederationProviderRepresentation
	if err := json.Unmarshal(respBody, &providers); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal user federation providers")
	}
	return providers, nil
}

func (c *keycloakClient) GetUserFederationProvider(ctx context.Context, realm, providerID string) (*UserFederationProviderRepresentation, error) {
	path := realmPath(realm) + "/user-federation/instances/" + url.PathEscape(providerID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var p UserFederationProviderRepresentation
	if err := json.Unmarshal(respBody, &p); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal user federation provider")
	}
	return &p, nil
}

func (c *keycloakClient) CreateUserFederationProvider(ctx context.Context, realm string, provider *UserFederationProviderRepresentation) (string, error) {
	path := realmPath(realm) + "/user-federation/instances"
	return c.doCreate(ctx, path, provider)
}

func (c *keycloakClient) UpdateUserFederationProvider(ctx context.Context, realm, providerID string, provider *UserFederationProviderRepresentation) error {
	path := realmPath(realm) + "/user-federation/instances/" + url.PathEscape(providerID)
	_, err := c.doRequest(ctx, http.MethodPut, path, provider)
	return err
}

func (c *keycloakClient) DeleteUserFederationProvider(ctx context.Context, realm, providerID string) error {
	path := realmPath(realm) + "/user-federation/instances/" + url.PathEscape(providerID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// =============================================================================
// Events Configuration Operations
// =============================================================================

type RealmEventsConfigRepresentation struct {
	EventsEnabled             *bool    `json:"eventsEnabled,omitempty"`
	EventsExpiration          *int64   `json:"eventsExpiration,omitempty"`
	EventsListeners           []string `json:"eventsListeners,omitempty"`
	EnabledEvents             []string `json:"enabledEvents,omitempty"`
	AdminEventsEnabled        *bool    `json:"adminEventsEnabled,omitempty"`
	AdminEventsDetailsEnabled *bool    `json:"adminEventsDetailsEnabled,omitempty"`
}

func (c *keycloakClient) GetRealmEventsConfig(ctx context.Context, realm string) (*RealmEventsConfigRepresentation, error) {
	path := realmPath(realm) + "/events/config"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var config RealmEventsConfigRepresentation
	if err := json.Unmarshal(respBody, &config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal events config")
	}
	return &config, nil
}

func (c *keycloakClient) UpdateRealmEventsConfig(ctx context.Context, realm string, config *RealmEventsConfigRepresentation) error {
	path := realmPath(realm) + "/events/config"
	_, err := c.doRequest(ctx, http.MethodPut, path, config)
	return err
}

// =============================================================================
// Realm Import Operations
// =============================================================================

func (c *keycloakClient) ImportRealm(ctx context.Context, realmJSON string, ifNotExists bool) error {
	path := adminPath + "/import"
	if ifNotExists {
		path += "?ifNotExists=true"
	}
	_, err := c.doRequest(ctx, http.MethodPost, path, realmJSON)
	return err
}

// =============================================================================
// Authorization (UMA) Operations
// =============================================================================

type AuthzResourceRepresentation struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	URIs        []string `json:"uri,omitempty"`
	Type        *string  `json:"type,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	DisplayName *string  `json:"displayName,omitempty"`
	IconURI     *string  `json:"iconUri,omitempty"`
}

func (c *keycloakClient) ListAuthzResources(ctx context.Context, realm, clientID string) ([]AuthzResourceRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var resources []AuthzResourceRepresentation
	if err := json.Unmarshal(respBody, &resources); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authz resources")
	}
	return resources, nil
}

func (c *keycloakClient) GetAuthzResource(ctx context.Context, realm, clientID, resourceID string) (*AuthzResourceRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource/" + url.PathEscape(resourceID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var r AuthzResourceRepresentation
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authz resource")
	}
	return &r, nil
}

func (c *keycloakClient) CreateAuthzResource(ctx context.Context, realm, clientID string, resource *AuthzResourceRepresentation) (string, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource"
	return c.doCreate(ctx, path, resource)
}

func (c *keycloakClient) UpdateAuthzResource(ctx context.Context, realm, clientID, resourceID string, resource *AuthzResourceRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource/" + url.PathEscape(resourceID)
	_, err := c.doRequest(ctx, http.MethodPut, path, resource)
	return err
}

func (c *keycloakClient) DeleteAuthzResource(ctx context.Context, realm, clientID, resourceID string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource/" + url.PathEscape(resourceID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// =============================================================================
// Client Certificate Operations
// =============================================================================

type ClientCertificateRepresentation struct {
	ID          string `json:"id,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	PrivateKey  string `json:"privateKey,omitempty"`
	Format      string `json:"format,omitempty"`
}

func (c *keycloakClient) ListClientCertificates(ctx context.Context, realm, clientID string) ([]ClientCertificateRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/certificates"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var certs []ClientCertificateRepresentation
	if err := json.Unmarshal(respBody, &certs); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client certificates")
	}
	return certs, nil
}

func (c *keycloakClient) GetClientCertificate(ctx context.Context, realm, clientID, certID string) (*ClientCertificateRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/certificates/" + url.PathEscape(certID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var cert ClientCertificateRepresentation
	if err := json.Unmarshal(respBody, &cert); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client certificate")
	}
	return &cert, nil
}

func (c *keycloakClient) GenerateClientCertificate(ctx context.Context, realm, clientID string, format string) (*ClientCertificateRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/certificates/generate"
	if format != "" {
		path += "?format=" + url.QueryEscape(format)
	}
	respBody, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	var cert ClientCertificateRepresentation
	if err := json.Unmarshal(respBody, &cert); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal generated client certificate")
	}
	return &cert, nil
}

// =============================================================================
// Role Mapping Operations
// =============================================================================

func (c *keycloakClient) ListUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string) ([]RoleRepresentation, error) {
	path := realmPath(realm) + "/users/" + url.PathEscape(userID) + "/role-mappings/clients/" + url.PathEscape(clientUUID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var roles []RoleRepresentation
	if err := json.Unmarshal(respBody, &roles); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal user client role mappings")
	}
	return roles, nil
}

func (c *keycloakClient) AddUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string, roles []RoleRepresentation) error {
	path := realmPath(realm) + "/users/" + url.PathEscape(userID) + "/role-mappings/clients/" + url.PathEscape(clientUUID)
	_, err := c.doRequest(ctx, http.MethodPost, path, roles)
	return err
}

func (c *keycloakClient) RemoveUserClientRoleMappings(ctx context.Context, realm, userID, clientUUID string, roles []RoleRepresentation) error {
	path := realmPath(realm) + "/users/" + url.PathEscape(userID) + "/role-mappings/clients/" + url.PathEscape(clientUUID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, roles)
	return err
}

func (c *keycloakClient) ListClientScopeMappings(ctx context.Context, realm, clientUUID string) ([]RoleRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/scope-mappings/realm"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var scopes []RoleRepresentation
	if err := json.Unmarshal(respBody, &scopes); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client scope mappings")
	}
	return scopes, nil
}

func (c *keycloakClient) AddClientScopeMappings(ctx context.Context, realm, clientUUID string, scopes []RoleRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/scope-mappings/realm"
	_, err := c.doRequest(ctx, http.MethodPost, path, scopes)
	return err
}

func (c *keycloakClient) RemoveClientScopeMappings(ctx context.Context, realm, clientUUID string, scopes []RoleRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/scope-mappings/realm"
	_, err := c.doRequest(ctx, http.MethodDelete, path, scopes)
	return err
}

// =============================================================================
// Client Initial Access Operations
// =============================================================================

type ClientInitialAccessRepresentation struct {
	ID             string `json:"id,omitempty"`
	Token          string `json:"token,omitempty"`
	Count          int32  `json:"count"`
	Expiration     int32  `json:"expiration"`
	Timestamp      int64  `json:"timestamp,omitempty"`
	RemainingCount int32  `json:"remainingCount,omitempty"`
}

func (c *keycloakClient) CreateClientInitialAccess(ctx context.Context, realm string, count, expiration int32) (*ClientInitialAccessRepresentation, error) {
	path := realmPath(realm) + "/clients-initial-access"
	body := map[string]interface{}{"count": count, "expiration": expiration}
	respBody, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	var cia ClientInitialAccessRepresentation
	if err := json.Unmarshal(respBody, &cia); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client initial access")
	}
	return &cia, nil
}

func (c *keycloakClient) ListClientInitialAccess(ctx context.Context, realm string) ([]ClientInitialAccessRepresentation, error) {
	path := realmPath(realm) + "/clients-initial-access"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var access []ClientInitialAccessRepresentation
	if err := json.Unmarshal(respBody, &access); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client initial access list")
	}
	return access, nil
}

func (c *keycloakClient) DeleteClientInitialAccess(ctx context.Context, realm, id string) error {
	path := realmPath(realm) + "/clients-initial-access/" + url.PathEscape(id)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

// =============================================================================
// Component Operations
// =============================================================================

type ComponentRepresentation struct {
	ID           string              `json:"id,omitempty"`
	Name         string              `json:"name"`
	ProviderType string              `json:"providerType"`
	ProviderID   string              `json:"providerId,omitempty"`
	SubType      string              `json:"subType,omitempty"`
	Config       map[string][]string `json:"config,omitempty"`
}

func (c *keycloakClient) GetComponent(ctx context.Context, realm, id string) (*ComponentRepresentation, error) {
	path := realmPath(realm) + "/components/" + url.PathEscape(id)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var comp ComponentRepresentation
	if err := json.Unmarshal(respBody, &comp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal component")
	}
	return &comp, nil
}

func (c *keycloakClient) CreateComponent(ctx context.Context, realm string, comp *ComponentRepresentation) (string, error) {
	id, err := c.doCreate(ctx, realmPath(realm)+"/components", comp)
	return id, err
}

func (c *keycloakClient) UpdateComponent(ctx context.Context, realm, id string, comp *ComponentRepresentation) error {
	path := realmPath(realm) + "/components/" + url.PathEscape(id)
	_, err := c.doRequest(ctx, http.MethodPut, path, comp)
	return err
}

func (c *keycloakClient) DeleteComponent(ctx context.Context, realm, id string) error {
	path := realmPath(realm) + "/components/" + url.PathEscape(id)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListComponentsByType(ctx context.Context, realm, providerType, name string) ([]ComponentRepresentation, error) {
	path := realmPath(realm) + "/components?type=" + url.QueryEscape(providerType)
	if name != "" {
		path += "&name=" + url.QueryEscape(name)
	}
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var comps []ComponentRepresentation
	if err := json.Unmarshal(respBody, &comps); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal components")
	}
	return comps, nil
}

// =============================================================================
// Realm Keys Operations
// =============================================================================

type KeyInfoRepresentation struct {
	Kid         string `json:"kid,omitempty"`
	Type        string `json:"type,omitempty"`
	Algorithm   string `json:"algorithm,omitempty"`
	Status      string `json:"status,omitempty"`
	Certificate string `json:"certificate,omitempty"`
}

type RealmKeysRepresentation struct {
	Active map[string]string       `json:"active,omitempty"`
	Keys   []KeyInfoRepresentation `json:"keys,omitempty"`
}

func (c *keycloakClient) GetRealmKeys(ctx context.Context, realm string) (*RealmKeysRepresentation, error) {
	path := realmPath(realm) + "/keys"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var keys RealmKeysRepresentation
	if err := json.Unmarshal(respBody, &keys); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal realm keys")
	}
	return &keys, nil
}

// =============================================================================
// Client Scope Operations
// =============================================================================

type ClientScopeRepresentation struct {
	ID                  string `json:"id,omitempty"`
	Name                string `json:"name,omitempty"`
	Description         string `json:"description,omitempty"`
	Protocol            string `json:"protocol,omitempty"`
	IncludeInTokenScope bool   `json:"includeInTokenScope,omitempty"`
}

func (c *keycloakClient) ListClientDefaultScopes(ctx context.Context, realm, clientUUID string) ([]ClientScopeRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/default-client-scopes"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var scopes []ClientScopeRepresentation
	if err := json.Unmarshal(respBody, &scopes); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client default scopes")
	}
	return scopes, nil
}

func (c *keycloakClient) AddClientDefaultScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/default-client-scopes"
	for _, s := range scopes {
		scopePath := path + "/" + url.PathEscape(s.ID)
		_, err := c.doRequest(ctx, http.MethodPut, scopePath, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *keycloakClient) RemoveClientDefaultScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/default-client-scopes"
	for _, s := range scopes {
		scopePath := path + "/" + url.PathEscape(s.ID)
		_, err := c.doRequest(ctx, http.MethodDelete, scopePath, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *keycloakClient) ListClientOptionalScopes(ctx context.Context, realm, clientUUID string) ([]ClientScopeRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/optional-client-scopes"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var scopes []ClientScopeRepresentation
	if err := json.Unmarshal(respBody, &scopes); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client optional scopes")
	}
	return scopes, nil
}

func (c *keycloakClient) AddClientOptionalScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/optional-client-scopes"
	for _, s := range scopes {
		scopePath := path + "/" + url.PathEscape(s.ID)
		_, err := c.doRequest(ctx, http.MethodPut, scopePath, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *keycloakClient) RemoveClientOptionalScopes(ctx context.Context, realm, clientUUID string, scopes []ClientScopeRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientUUID) + "/optional-client-scopes"
	for _, s := range scopes {
		scopePath := path + "/" + url.PathEscape(s.ID)
		_, err := c.doRequest(ctx, http.MethodDelete, scopePath, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Identity Provider Operations
// =============================================================================

type IdentityProviderRepresentation struct {
	InternalID                string            `json:"internalId,omitempty"`
	Alias                     string            `json:"alias"`
	DisplayName               string            `json:"displayName,omitempty"`
	ProviderId                string            `json:"providerId"`
	Enabled                   bool              `json:"enabled"`
	TrustEmail                bool              `json:"trustEmail,omitempty"`
	FirstBrokerLoginFlowAlias string            `json:"firstBrokerLoginFlowAlias,omitempty"`
	PostBrokerLoginFlowAlias  string            `json:"postBrokerLoginFlowAlias,omitempty"`
	Config                    map[string]string `json:"config,omitempty"`
}

func (c *keycloakClient) GetIdentityProvider(ctx context.Context, realm, alias string) (*IdentityProviderRepresentation, error) {
	path := realmPath(realm) + "/identity-provider/instances/" + url.PathEscape(alias)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var idp IdentityProviderRepresentation
	if err := json.Unmarshal(respBody, &idp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal identity provider")
	}
	return &idp, nil
}

func (c *keycloakClient) CreateIdentityProvider(ctx context.Context, realm string, provider *IdentityProviderRepresentation) (string, error) {
	path := realmPath(realm) + "/identity-provider/instances"
	id, err := c.doCreate(ctx, path, provider)
	return id, err
}

func (c *keycloakClient) UpdateIdentityProvider(ctx context.Context, realm, alias string, provider *IdentityProviderRepresentation) error {
	path := realmPath(realm) + "/identity-provider/instances/" + url.PathEscape(alias)
	_, err := c.doRequest(ctx, http.MethodPut, path, provider)
	return err
}

func (c *keycloakClient) DeleteIdentityProvider(ctx context.Context, realm, alias string) error {
	path := realmPath(realm) + "/identity-provider/instances/" + url.PathEscape(alias)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListIdentityProviders(ctx context.Context, realm string) ([]IdentityProviderRepresentation, error) {
	path := realmPath(realm) + "/identity-provider/instances"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var providers []IdentityProviderRepresentation
	if err := json.Unmarshal(respBody, &providers); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal identity providers")
	}
	return providers, nil
}

// =============================================================================
// Authentication Flow Operations
// =============================================================================

type AuthenticationFlowRepresentation struct {
	ID          string `json:"id,omitempty"`
	Alias       string `json:"alias"`
	Description string `json:"description,omitempty"`
	ProviderId  string `json:"providerId"`
	BuiltIn     bool   `json:"builtIn,omitempty"`
	TopLevel    bool   `json:"topLevel,omitempty"`
}

func (c *keycloakClient) GetAuthenticationFlow(ctx context.Context, realm, alias string) (*AuthenticationFlowRepresentation, error) {
	path := realmPath(realm) + "/authentication/flows/" + url.PathEscape(alias)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var flow AuthenticationFlowRepresentation
	if err := json.Unmarshal(respBody, &flow); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authentication flow")
	}
	return &flow, nil
}

func (c *keycloakClient) CreateAuthenticationFlow(ctx context.Context, realm string, flow *AuthenticationFlowRepresentation) (string, error) {
	path := realmPath(realm) + "/authentication/flows"
	id, err := c.doCreate(ctx, path, flow)
	return id, err
}

func (c *keycloakClient) UpdateAuthenticationFlow(ctx context.Context, realm, alias string, flow *AuthenticationFlowRepresentation) error {
	path := realmPath(realm) + "/authentication/flows/" + url.PathEscape(alias)
	_, err := c.doRequest(ctx, http.MethodPut, path, flow)
	return err
}

func (c *keycloakClient) DeleteAuthenticationFlow(ctx context.Context, realm, alias string) error {
	path := realmPath(realm) + "/authentication/flows/" + url.PathEscape(alias)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListAuthenticationFlows(ctx context.Context, realm string) ([]AuthenticationFlowRepresentation, error) {
	path := realmPath(realm) + "/authentication/flows"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var flows []AuthenticationFlowRepresentation
	if err := json.Unmarshal(respBody, &flows); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authentication flows")
	}
	return flows, nil
}

// =============================================================================
// Authorization Policy Operations
// =============================================================================

type AuthorizationPolicyRepresentation struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Logic       string            `json:"logic,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

func (c *keycloakClient) GetAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string) (*AuthorizationPolicyRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource-server/policy/" + url.PathEscape(policyID)
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var policy AuthorizationPolicyRepresentation
	if err := json.Unmarshal(respBody, &policy); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authorization policy")
	}
	return &policy, nil
}

func (c *keycloakClient) CreateAuthorizationPolicy(ctx context.Context, realm, clientID string, policy *AuthorizationPolicyRepresentation) (string, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource-server/policy"
	id, err := c.doCreate(ctx, path, policy)
	return id, err
}

func (c *keycloakClient) UpdateAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string, policy *AuthorizationPolicyRepresentation) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource-server/policy/" + url.PathEscape(policyID)
	_, err := c.doRequest(ctx, http.MethodPut, path, policy)
	return err
}

func (c *keycloakClient) DeleteAuthorizationPolicy(ctx context.Context, realm, clientID, policyID string) error {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource-server/policy/" + url.PathEscape(policyID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *keycloakClient) ListAuthorizationPolicies(ctx context.Context, realm, clientID string) ([]AuthorizationPolicyRepresentation, error) {
	path := realmPath(realm) + "/clients/" + url.PathEscape(clientID) + "/authz/resource-server/policy"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var policies []AuthorizationPolicyRepresentation
	if err := json.Unmarshal(respBody, &policies); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal authorization policies")
	}
	return policies, nil
}

func (c *keycloakClient) ListClientScopes(ctx context.Context, realm string) ([]ClientScopeRepresentation, error) {
	path := realmPath(realm) + "/client-scopes"
	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var scopes []ClientScopeRepresentation
	if err := json.Unmarshal(respBody, &scopes); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal client scopes")
	}
	return scopes, nil
}

func (c *keycloakClient) GetClientScope(ctx context.Context, realm, name string) (*ClientScopeRepresentation, error) {
	// Keycloak's GET /client-scopes/{id} requires the internal UUID, not
	// the scope name, so list all scopes and filter by name.
	scopes, err := c.ListClientScopes(ctx, realm)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	for i := range scopes {
		if scopes[i].Name == name {
			return &scopes[i], nil
		}
	}
	return nil, nil
}

func (c *keycloakClient) CreateClientScope(ctx context.Context, realm string, scope ClientScopeRepresentation) error {
	path := realmPath(realm) + "/client-scopes"
	_, err := c.doRequest(ctx, http.MethodPost, path, scope)
	return err
}

func (c *keycloakClient) UpdateClientScope(ctx context.Context, realm string, scope ClientScopeRepresentation) error {
	// Keycloak's PUT /client-scopes/{id} requires the internal UUID.
	id := scope.ID
	if id == "" {
		existing, err := c.GetClientScope(ctx, realm, scope.Name)
		if err != nil {
			return err
		}
		if existing == nil {
			return errors.Errorf("client scope %q not found in realm %q", scope.Name, realm)
		}
		id = existing.ID
	}
	path := realmPath(realm) + "/client-scopes/" + url.PathEscape(id)
	_, err := c.doRequest(ctx, http.MethodPut, path, scope)
	return err
}

func (c *keycloakClient) DeleteClientScope(ctx context.Context, realm, name string) error {
	// Keycloak's DELETE /client-scopes/{id} requires the internal UUID.
	existing, err := c.GetClientScope(ctx, realm, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	path := realmPath(realm) + "/client-scopes/" + url.PathEscape(existing.ID)
	_, err = c.doRequest(ctx, http.MethodDelete, path, nil)
	return err
}
