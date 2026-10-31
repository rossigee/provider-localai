package v1beta1

import (
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPServerSpec is a remote MCP server the agent may use.
type MCPServerSpec struct {
	// URL of the MCP Streamable HTTP / SSE endpoint.
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Token is an optional bearer token. Prefer proxy sidecars that inject
	// tokens so this field stays empty in Git.
	// +optional
	Token string `json:"token,omitempty"`
}

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

	// Skills is the list of skill names available to the agent. It is sent as
	// selected_skills. LocalAI builds whose agent API has no selected_skills
	// field drop it on write, which leaves every skill available, so it is not
	// part of the drift comparison.
	// +optional
	Skills []string `json:"skills,omitempty"`

	// EnableSkills turns on the LocalAGI skills service for this agent.
	// +optional
	EnableSkills *bool `json:"enableSkills,omitempty"`

	// MCPServers is the per-agent MCP allowlist. Agents do not inherit
	// model-level mcp.remote from LocalAI model configs.
	// +optional
	MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`

	// AllowedTools, when set, is the only tool names offered to the model.
	// +optional
	AllowedTools []string `json:"allowedTools,omitempty"`

	// ExcludedTools are never offered, even if also listed in AllowedTools.
	// +optional
	ExcludedTools []string `json:"excludedTools,omitempty"`

	// EnableKB turns on the knowledge base for this agent.
	// +optional
	EnableKB *bool `json:"enableKB,omitempty"`

	// KbAsTools exposes the knowledge base as MCP tools.
	// +optional
	KbAsTools *bool `json:"kbAsTools,omitempty"`

	// LongTermMemory enables long-term memory (notes) for this agent.
	// +optional
	LongTermMemory *bool `json:"longTermMemory,omitempty"`

	// APIKeySecretRef references a Secret key holding the LocalAI API key the
	// agent uses to call LocalAI's own API. Required when LocalAI has
	// authentication enabled: LocalAI only generates a per-user agent key for
	// callers that have a row in the users table, and a provider authenticating
	// with a static API key has none, so creation would otherwise fail with a
	// user_api_keys foreign key violation.
	// +optional
	APIKeySecretRef *xpv1.SecretKeySelector `json:"apiKeySecretRef,omitempty"`
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
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
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
