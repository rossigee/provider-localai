package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderConfigSpec defines the desired state of a ProviderConfig.
type ProviderConfigSpec struct {
	// Credentials required to authenticate to this provider. The referenced
	// secret holds a LocalAI API key, which is sent as a bearer token. This is
	// the same credential shape the other providers in this cluster use.
	Credentials *ProviderCredentials `json:"credentials,omitempty"`

	// BaseURL is the LocalAI base URL.
	BaseURL string `json:"baseURL"`

	// TokenURL is the OAuth2 token endpoint (Keycloak).
	// Deprecated: LocalAI's API does not accept OIDC access tokens. Use
	// Credentials with a LocalAI API key instead.
	TokenURL string `json:"tokenURL,omitempty"`
	// ClientID for client credentials.
	// Deprecated: use Credentials with a LocalAI API key instead.
	ClientID string `json:"clientID,omitempty"`
	// ClientSecretRef references the secret holding client secret.
	// Deprecated: use Credentials with a LocalAI API key instead.
	ClientSecretRef *xpv1.SecretKeySelector `json:"clientSecretRef,omitempty"`

	// RootCACertificateRef references a secret containing the CA certificate(s)
	// to trust when connecting to LocalAI. If omitted, the system CA pool is used.
	RootCACertificateRef *xpv1.SecretKeySelector `json:"rootCACertificateRef,omitempty"`
}

// ProviderCredentials required to authenticate.
type ProviderCredentials struct {
	// Source of the provider credentials.
	// +kubebuilder:validation:Enum=Secret
	Source xpv1.CredentialsSource `json:"source"`

	xpv1.CommonCredentialSelectors `json:",inline"`
}

// ProviderConfigStatus defines the observed state.
type ProviderConfigStatus struct {
	xpv1.ProviderConfigStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProviderConfigSpec   `json:"spec"`
	Status            ProviderConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

// +kubebuilder:object:root=true
type ProviderConfigUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}
