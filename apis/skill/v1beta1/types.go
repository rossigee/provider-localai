package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
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

	// Content is the skill's instructions, sent to LocalAI as content.
	// +kubebuilder:validation:Required
	Content string `json:"content"`
}

// SkillObservation is the observed state.
type SkillObservation struct {
	Name string `json:"name,omitempty"`
	// Ownership is "created" when this provider created the skill in LocalAI,
	// or "adopted" when it was already there on first observation. Deleting the
	// resource deletes the skill only when it is not adopted.
	Ownership string `json:"ownership,omitempty"`
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
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
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

// GetItems returns the Skill resources in the list.
func (l *SkillList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
