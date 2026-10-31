package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SettingsParameters struct {
	// +optional
	BaseURL string `json:"baseURL,omitempty"`
}

type SettingsObservation struct{}

type SettingsSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              SettingsParameters `json:"forProvider"`
}

type SettingsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 SettingsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
type Settings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SettingsSpec   `json:"spec"`
	Status            SettingsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Settings `json:"items"`
}
