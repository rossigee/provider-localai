package v1alpha1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentParameters define the desired state of a LocalAI Agent.
type AgentParameters struct {
	// Name of the agent in LocalAI.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Model to use for this agent.
	// +kubebuilder:validation:Required
	Model string `json:"model"`

	// Description of the agent.
	// +optional
	Description string `json:"description,omitempty"`

	// SystemPrompt is the system prompt for the agent.
	// +optional
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// Skills is the list of skill names available to the agent.
	// +optional
	Skills []string `json:"skills,omitempty"`
}

// AgentObservation is the observed state of a LocalAI Agent.
type AgentObservation struct {
	// Name of the agent.
	Name string `json:"name,omitempty"`
}

// AgentSpec defines the desired state of an Agent.
type AgentSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              AgentParameters `json:"forProvider"`
}

// AgentStatus defines the observed state of an Agent.
type AgentStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 AgentObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentSpec   `json:"spec"`
	Status            AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}
