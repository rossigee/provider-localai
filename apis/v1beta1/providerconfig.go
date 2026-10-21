package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderConfigSpec defines the desired state of a ProviderConfig.
type ProviderConfigSpec struct {
	// BaseURL is the LocalAI base URL.
	BaseURL string `json:"baseURL"`
	// TokenURL is the OAuth2 token endpoint (Keycloak).
	TokenURL string `json:"tokenURL"`
	// ClientID for client credentials.
	ClientID string `json:"clientID"`
	// ClientSecretRef references the secret holding client secret.
	ClientSecretRef *xpv1.SecretKeySelector `json:"clientSecretRef,omitempty"`
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
