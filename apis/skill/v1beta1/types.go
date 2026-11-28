package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SkillParameters are the desired settings of a LocalAI skill. A skill is
// identified by its layer and name.
type SkillParameters struct {
	// Layer is the skill layer: personal skills belong to the service user,
	// shared skills are visible to every user, and project skills belong to a
	// project and are always on for its chats and agents.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=personal;shared;project
	Layer string `json:"layer"`

	// ProjectID is the LocalAI ID of the project that owns a project-layer
	// skill. Required when layer is project, ignored otherwise.
	// +optional
	ProjectID string `json:"projectId,omitempty"`

	// Name is the skill's name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Description is a short description of the skill.
	// +optional
	Description string `json:"description,omitempty"`

	// Content is the skill's text, its instructions.
	// +kubebuilder:validation:Required
	Content string `json:"content"`
}

// SkillObservation is the state LocalAI reports for the skill.
type SkillObservation struct {
	// Layer is the layer the skill is stored in.
	Layer string `json:"layer,omitempty"`

	// Name is the skill's name as LocalAI reports it.
	Name string `json:"name,omitempty"`

	// ProjectID is the project that owns a project-layer skill.
	ProjectID string `json:"projectId,omitempty"`
}

// SkillSpec is the desired state of a Skill.
type SkillSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              SkillParameters `json:"forProvider"`
}

// SkillStatus is the observed state of a Skill.
type SkillStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 SkillObservation `json:"atProvider,omitempty"`
}

// A Skill is a LocalAI skill, owned by the service user behind the
// ProviderConfig's API key.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Skill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SkillSpec   `json:"spec"`
	Status            SkillStatus `json:"status,omitempty"`
}

// SkillList contains a list of Skills.
// +kubebuilder:object:root=true
type SkillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Skill `json:"items"`
}

// GetItems returns the Skills as managed resources.
func (l *SkillList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
