package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentFolder chooses the folder a saved agent works in. Omit it for an agent
// that works on all the user's files.
type AgentFolder struct {
	// Path is the folder the agent works in.
	Path string `json:"path"`

	// Mode is the folder access mode LocalAI applies when it is set.
	// +optional
	Mode string `json:"mode,omitempty"`
}

// AgentParameters are the desired settings of a saved LocalAI agent.
type AgentParameters struct {
	// Name is the agent's display name.
	Name string `json:"name"`

	// Description is a short description of the agent.
	// +optional
	Description string `json:"description,omitempty"`

	// Instructions is the system prompt the agent runs with.
	// +optional
	Instructions string `json:"instructions,omitempty"`

	// Model is the LocalAI model the agent uses.
	// +optional
	Model string `json:"model,omitempty"`

	// Skills lists the names of the skills the agent may use.
	// +optional
	Skills []string `json:"skills,omitempty"`

	// Collections lists the names of the knowledge-base collections the agent
	// searches.
	// +optional
	Collections []string `json:"collections,omitempty"`

	// MCPServers lists the names of the MCP servers the agent may use, as they
	// are configured in the model's MCP configuration.
	// +optional
	MCPServers []string `json:"mcpServers,omitempty"`

	// Addons lists the IDs of the add-ons the agent may use.
	// +optional
	Addons []string `json:"addons,omitempty"`

	// Network is LocalAI's network preset for the agent. Empty means off.
	// +optional
	Network string `json:"network,omitempty"`

	// Folder chooses the folder the agent works in.
	// +optional
	Folder *AgentFolder `json:"folder,omitempty"`
}

// AgentObservation is the state LocalAI reports for the agent.
type AgentObservation struct {
	// ID is the LocalAI identifier of the agent. It is also the agent's
	// external name.
	ID string `json:"id,omitempty"`
}

// AgentSpec is the desired state of an Agent.
type AgentSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              AgentParameters `json:"forProvider"`
}

// AgentStatus is the observed state of an Agent.
type AgentStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 AgentObservation `json:"atProvider,omitempty"`
}

// An Agent is a saved LocalAI agent, owned by the service user behind the
// ProviderConfig's API key.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentSpec   `json:"spec"`
	Status            AgentStatus `json:"status,omitempty"`
}

// AgentList contains a list of Agents.
// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

// GetItems returns the Agents as managed resources.
func (l *AgentList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
