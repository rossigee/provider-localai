package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TriggerParameters are the desired settings of a LocalAI chat trigger. A
// trigger runs a prompt on a schedule, through a saved agent or a model.
type TriggerParameters struct {
	// Name is the trigger's display name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Prompt is the prompt the trigger runs.
	// +kubebuilder:validation:Required
	Prompt string `json:"prompt"`

	// Kind is the trigger kind. Only "schedule" is managed.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=schedule
	Kind string `json:"kind"`

	// Schedule is a LocalAI schedule expression, such as "daily 08:00".
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// TimeZone is the IANA time zone the schedule is read in.
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// DailyCap limits runs per day. Unset leaves LocalAI's default, which is
	// not compared on drift.
	// +optional
	DailyCap int `json:"dailyCap,omitempty"`

	// AgentID is the LocalAI ID of the saved agent the trigger runs as.
	// +optional
	AgentID string `json:"agentId,omitempty"`

	// Model is the LocalAI model the trigger runs on.
	// +optional
	Model string `json:"model,omitempty"`

	// Enabled, when unset, is enabled, LocalAI's default.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// TriggerObservation is the state LocalAI reports for the trigger.
type TriggerObservation struct {
	// ID is the LocalAI identifier of the trigger. It is also the external name.
	ID string `json:"id,omitempty"`
}

// TriggerSpec is the desired state of a Trigger.
type TriggerSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              TriggerParameters `json:"forProvider"`
}

// TriggerStatus is the observed state of a Trigger.
type TriggerStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 TriggerObservation `json:"atProvider,omitempty"`
}

// A Trigger is a LocalAI chat trigger that runs a prompt on a schedule.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Trigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TriggerSpec   `json:"spec"`
	Status            TriggerStatus `json:"status,omitempty"`
}

// TriggerList contains a list of Triggers.
// +kubebuilder:object:root=true
type TriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Trigger `json:"items"`
}

// GetItems returns the Triggers as managed resources.
func (l *TriggerList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
