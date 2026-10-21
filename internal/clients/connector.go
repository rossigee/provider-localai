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
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/singleflight"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rossigee/provider-keycloak/apis/v1beta1"
)

// Connector caches a *keycloakClient per ProviderConfig so that the
// expensive OAuth2 token handshake is performed once per PC rather than
// once per managed-resource reconcile. Failed connection attempts are also
// cached and short-circuited with an exponential backoff so that an
// unreachable Keycloak upstream does not produce a per-CR token storm.
type Connector struct {
	kube client.Client

	mu      sync.RWMutex
	entries map[string]*connectorEntry

	// singleflight groups concurrent Connect calls for the same PC so
	// only one goroutine performs the OAuth2 handshake at a time.
	sf singleflight.Group

	// backoff parameters (overridable for tests).
	connectBackoffInitial time.Duration
	connectBackoffMax     time.Duration
}

type connectorEntry struct {
	client           *keycloakClient
	lastErr          error
	lastAttemptAt    time.Time
	consecutiveFails int
	backoffUntil     time.Time
}

// NewConnector returns a Connector that resolves ProviderConfigs from the
// provided Kubernetes client.
func NewConnector(kube client.Client) *Connector {
	return &Connector{
		kube:                  kube,
		entries:               make(map[string]*connectorEntry),
		connectBackoffInitial: 5 * time.Second,
		connectBackoffMax:     5 * time.Minute,
	}
}

// Connect returns a Client for the named ProviderConfig. It caches the
// resulting *keycloakClient so subsequent calls reuse the cached token. If
// a previous attempt failed and we are still inside the backoff window,
// the cached error is returned wrapped in ErrAuthUnavailable.
func (c *Connector) Connect(ctx context.Context, pcName string) (Client, error) {
	now := time.Now()

	// Fast path: cache hit, fresh token, no recent failure.
	c.mu.RLock()
	entry, ok := c.entries[pcName]
	c.mu.RUnlock()
	if ok && entry != nil && entry.client != nil && entry.lastErr == nil {
		if now.Before(entry.client.tokenExp) {
			return entry.client, nil
		}
	}

	// Slow path: dedupe concurrent attempts, then either retry or
	// short-circuit on backoff.
	v, err, _ := c.sf.Do(pcName, func() (interface{}, error) {
		c.mu.Lock()
		entry, ok := c.entries[pcName]
		c.mu.Unlock()
		if ok && entry != nil && entry.lastErr != nil && now.Before(entry.backoffUntil) {
			return nil, errors.Wrapf(ErrAuthUnavailable, "Keycloak connector in backoff until %s (last error: %v)", entry.backoffUntil.Format(time.RFC3339), entry.lastErr)
		}

		// We need the actual ProviderConfig object; fetch it from the API.
		pc := &v1beta1.ProviderConfig{}
		if err := c.kube.Get(ctx, client.ObjectKey{Name: pcName}, pc); err != nil {
			c.recordFailure(pcName, errors.Wrap(err, "cannot get ProviderConfig"))
			return nil, errors.Wrap(ErrAuthUnavailable, err.Error())
		}

		kc, err := NewClient(ctx, pc, c.kube)
		if err != nil {
			c.recordFailure(pcName, err)
			return nil, errors.Wrap(ErrAuthUnavailable, err.Error())
		}

		c.recordSuccess(pcName, kc)
		return kc, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(Client), nil
}

// recordFailure updates the cached failure state for pcName.
func (c *Connector) recordFailure(pcName string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[pcName]
	if !ok {
		entry = &connectorEntry{}
		c.entries[pcName] = entry
	}
	entry.lastErr = err
	entry.lastAttemptAt = time.Now()
	entry.consecutiveFails++
	delay := c.connectBackoffInitial << (entry.consecutiveFails - 1)
	if delay <= 0 || delay > c.connectBackoffMax {
		delay = c.connectBackoffMax
	}
	entry.backoffUntil = entry.lastAttemptAt.Add(delay)
}

// recordSuccess caches a freshly built client.
func (c *Connector) recordSuccess(pcName string, kc *keycloakClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[pcName] = &connectorEntry{
		client:        kc,
		lastAttemptAt: time.Now(),
	}
}

// Invalidate clears the cached entry for pcName. Use this when a CR's
// ProviderConfig reference changes or the credentials secret is rotated.
func (c *Connector) Invalidate(pcName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, pcName)
}
