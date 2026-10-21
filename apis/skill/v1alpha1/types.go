package v1alpha1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkillParameters define the desired state of a LocalAI Skill.
type SkillParameters struct {
	// Name of the skill in LocalAI.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Description of the skill.
	// +optional
	Description string `json:"description,omitempty"`
}

// SkillObservation is the observed state.
type SkillObservation struct {
	Name string `json:"name,omitempty"`
}

// SkillSpec defines the desired state.
type SkillSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              SkillParameters `json:"forProvider"`
}

// SkillStatus defines the observed state.
type SkillStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 SkillObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,localai}
type Skill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SkillSpec   `json:"spec"`
	Status            SkillStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SkillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Skill `json:"items"`
}
