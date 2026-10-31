package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ModelParameters struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +optional
	Backend string `json:"backend,omitempty"`
}

type ModelObservation struct {
	Name string `json:"name,omitempty"`
}

type ModelSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              ModelParameters `json:"forProvider"`
}

type ModelStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ModelObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
type Model struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ModelSpec   `json:"spec"`
	Status            ModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Model `json:"items"`
}
