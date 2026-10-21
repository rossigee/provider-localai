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
	"encoding/json"
	"net/url"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rossigee/provider-keycloak/apis/v1beta1"
)

// ProviderCredentials holds the parsed connection details from the credentials secret.
// The secret value must be a JSON object with these keys.
type ProviderCredentials struct {
	URL                   string `json:"url"`
	BasePath              string `json:"base_path"`
	Realm                 string `json:"realm"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	RootCACertificate     string `json:"root_ca_certificate"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	AdminAPIv2Enabled     bool   `json:"admin_api_v2_enabled"` // experimental: enable Keycloak 26.8+ Admin API v2
}

// Config contains the resolved connection details for the Keycloak API.
type Config struct {
	BaseURL               string
	Realm                 string
	ClientID              string
	ClientSecret          string
	RootCACertificate     string
	TLSInsecureSkipVerify bool
	Username              string
	Password              string
	AdminAPIv2Enabled     bool // experimental: enable Keycloak 26.8+ Admin API v2
}

// GetConfig extracts the Keycloak connection config from a ProviderConfig.
// The credentials secret must contain a JSON blob under the referenced key.
func GetConfig(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) (*Config, error) {
	raw, err := readCredentialsSecret(ctx, pc, kube)
	if err != nil {
		return nil, err
	}
	return parseCredentials(raw)
}

func readCredentialsSecret(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) ([]byte, error) {
	if pc.Spec.Credentials.SecretRef == nil {
		return nil, errors.New("credentials.secretRef is required")
	}
	secret := &corev1.Secret{}
	nn := types.NamespacedName{
		Name:      pc.Spec.Credentials.SecretRef.Name,
		Namespace: pc.Spec.Credentials.SecretRef.Namespace,
	}
	if err := kube.Get(ctx, nn, secret); err != nil {
		return nil, errors.Wrap(err, "cannot get credentials secret")
	}
	key := pc.Spec.Credentials.SecretRef.Key
	if key == "" {
		key = "credentials"
	}
	raw, ok := secret.Data[key]
	if !ok {
		return nil, errors.Errorf("key %q not found in credentials secret", key)
	}
	return raw, nil
}

func parseCredentials(raw []byte) (*Config, error) {
	var creds ProviderCredentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, errors.Wrap(err, "cannot parse credentials JSON")
	}
	if err := validateCredentials(&creds); err != nil {
		return nil, err
	}
	applyDefaults(&creds)
	return &Config{
		BaseURL:               creds.URL + creds.BasePath,
		Realm:                 creds.Realm,
		ClientID:              creds.ClientID,
		ClientSecret:          creds.ClientSecret,
		RootCACertificate:     creds.RootCACertificate,
		TLSInsecureSkipVerify: creds.TLSInsecureSkipVerify,
		Username:              creds.Username,
		Password:              creds.Password,
		AdminAPIv2Enabled:     creds.AdminAPIv2Enabled,
	}, nil
}

func validateCredentials(creds *ProviderCredentials) error {
	if creds.URL == "" {
		return errors.New("credentials JSON missing required field: url")
	}
	parsed, err := url.Parse(creds.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("credentials: url must be a valid http or https URL")
	}
	if creds.ClientID == "" {
		return errors.New("credentials JSON missing required field: client_id")
	}
	hasClientSecret := creds.ClientSecret != ""
	hasUserPass := creds.Username != "" && creds.Password != ""
	if !hasClientSecret && !hasUserPass {
		return errors.New("credentials must include either client_secret or both username and password")
	}
	return nil
}

func applyDefaults(creds *ProviderCredentials) {
	if creds.Realm == "" {
		creds.Realm = defaultRealm
	}
	if creds.BasePath == "" {
		creds.BasePath = defaultBasePath
	}
}

const defaultRealm = "master"
const defaultBasePath = "/auth"
