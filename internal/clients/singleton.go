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
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	connectorOnce sync.Once
	connector     *Connector
)

// GetConnector returns the process-wide Connector, lazily initialised on
// first call. All Keycloak controllers share this Connector so that one
// successful token fetch per ProviderConfig is reused across every
// managed resource reconcile, and a failing Keycloak upstream is
// backpressured through a single exponential-backoff schedule rather
// than triggering one token request per CR per reconcile.
func GetConnector(kube client.Client) *Connector {
	connectorOnce.Do(func() {
		connector = NewConnector(kube)
	})
	return connector
}

// ResetConnector clears the cached Connector. Intended for tests only.
func ResetConnector() {
	connector = nil
	connectorOnce = sync.Once{}
}
