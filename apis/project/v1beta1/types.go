package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectDefaults are the default tool lists for new chats in a project.
type ProjectDefaults struct {
	// +optional
	Skills []string `json:"skills,omitempty"`
	// +optional
	Addons []string `json:"addons,omitempty"`
	// +optional
	Connectors []string `json:"connectors,omitempty"`
	// +optional
	MCPServers []string `json:"mcpServers,omitempty"`
	// +optional
	Collections []string `json:"collections,omitempty"`
}

// ProjectAttached are the always-on (attached) tool lists for a project.
type ProjectAttached struct {
	// +optional
	Skills []string `json:"skills,omitempty"`
	// +optional
	Addons []string `json:"addons,omitempty"`
	// +optional
	Connectors []string `json:"connectors,omitempty"`
	// +optional
	MCPServers []string `json:"mcpServers,omitempty"`
	// +optional
	Collections []string `json:"collections,omitempty"`
}

// ProjectParameters are the desired settings of a LocalAI project.
type ProjectParameters struct {
	// Name is the project's name in LocalAI.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Description is the project description (first 200 chars given to chats as context).
	// +optional
	Description string `json:"description,omitempty"`

	// Owner is the LocalAI user id to act on behalf of. When set, the provider
	// passes user_id to LocalAI project endpoints (admin scope). Leave empty to
	// manage projects owned by the API key identity.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Defaults are the tool defaults applied to new chats in the project.
	// +optional
	Defaults *ProjectDefaults `json:"defaults,omitempty"`

	// Attached are the always-on (attached) tools for the project.
	// +optional
	Attached *ProjectAttached `json:"attached,omitempty"`

	// Archived, when set to true, archives the project.
	// +optional
	Archived *bool `json:"archived,omitempty"`
}

// ProjectObservation is the state LocalAI reports for the project.
type ProjectObservation struct {
	// ID is the project's slug (folder name) in LocalAI.
	ID string `json:"id,omitempty"`
	// Name is the project's name as LocalAI reports it.
	Name string `json:"name,omitempty"`
	// Owner is the LocalAI user id that owns the project.
	Owner string `json:"owner,omitempty"`
	// Status is the project status (active, archived, broken).
	Status string `json:"status,omitempty"`
	// ChatCount is the number of chats in the project.
	ChatCount int `json:"chatCount,omitempty"`
}

// ProjectSpec is the desired state of a Project.
type ProjectSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              ProjectParameters `json:"forProvider"`
}

// ProjectStatus is the observed state of a Project.
type ProjectStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 ProjectObservation `json:"atProvider,omitempty"`
}

// A Project is a LocalAI project that groups chats around a folder.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProjectSpec   `json:"spec"`
	Status            ProjectStatus `json:"status,omitempty"`
}

// ProjectList contains a list of Projects.
// +kubebuilder:object:root=true
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

// GetItems returns the Projects as managed resources.
func (l *ProjectList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
