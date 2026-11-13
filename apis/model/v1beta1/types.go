package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ModelParameters struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +optional
	Backend string `json:"backend,omitempty"`
	// Config is the full model configuration as YAML, in the shape LocalAI
	// stores under /models (the HelmRelease modelsConfigs literals). It is
	// written verbatim via POST /models/import; re-importing overwrites.
	// When empty, LocalAI receives only name and backend. If the config
	// carries its own name it must match Name; name and backend in the CR
	// are authoritative.
	// +optional
	Config string `json:"config,omitempty"`
}

type ModelObservation struct {
	Name string `json:"name,omitempty"`
	// Ownership is "created" when this provider imported the model, or
	// "adopted" when it was already in LocalAI on first observation. Deleting
	// the resource deletes the model only when it is not adopted.
	Ownership string `json:"ownership,omitempty"`
	// ConfigHash is the sha256 of the canonical JSON of the configuration the
	// provider last applied, used to detect spec drift because LocalAI has no
	// endpoint that returns a model's stored configuration.
	ConfigHash string `json:"configHash,omitempty"`
	// PendingImportUUID is the job ID LocalAI returned for an import that has
	// not yet appeared in the model list. While it is set, Observe waits for
	// the model rather than re-posting the import on every poll.
	PendingImportUUID string `json:"pendingImportUUID,omitempty"`
	// PendingImportSince is the RFC 3339 time the pending import was submitted.
	// After a timeout the import is treated as failed and submitted again.
	PendingImportSince string `json:"pendingImportSince,omitempty"`
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

// GetItems returns the Model resources in the list.
func (l *ModelList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
