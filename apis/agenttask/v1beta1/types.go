package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentTaskParameters are the desired settings of a saved LocalAI agent task: a
// prompt template run by a model, or by a saved agent.
type AgentTaskParameters struct {
	// Name is the task's display name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Description is a short description of the task.
	// +optional
	Description string `json:"description,omitempty"`

	// Model is the LocalAI model the task runs on.
	// +optional
	Model string `json:"model,omitempty"`

	// AgentID is the LocalAI ID of the saved agent the task runs as.
	// +optional
	AgentID string `json:"agentId,omitempty"`

	// Prompt is the template prompt. Parameters are written as {{ .name }}.
	// +kubebuilder:validation:Required
	Prompt string `json:"prompt"`

	// Enabled, when unset, is enabled, LocalAI's default.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// AgentTaskObservation is the state LocalAI reports for the task.
type AgentTaskObservation struct {
	// ID is the LocalAI identifier of the task. It is also the external name.
	ID string `json:"id,omitempty"`
}

// AgentTaskSpec is the desired state of an AgentTask.
type AgentTaskSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              AgentTaskParameters `json:"forProvider"`
}

// AgentTaskStatus is the observed state of an AgentTask.
type AgentTaskStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 AgentTaskObservation `json:"atProvider,omitempty"`
}

// An AgentTask is a saved LocalAI agent task. Cron schedules, multimedia
// sources and webhooks are not managed here: LocalAI does not accept cron on a
// task, and webhook headers are hidden on read so they cannot be compared.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type AgentTask struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentTaskSpec   `json:"spec"`
	Status            AgentTaskStatus `json:"status,omitempty"`
}

// AgentTaskList contains a list of AgentTasks.
// +kubebuilder:object:root=true
type AgentTaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentTask `json:"items"`
}

// GetItems returns the AgentTasks as managed resources.
func (l *AgentTaskList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
