package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackendParameters are the desired state of a LocalAI backend. The backend is
// named by its gallery entry.
type BackendParameters struct {
	// Name is the gallery entry name of the backend, such as "llama-cpp".
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// BackendObservation is the state of the backend install or delete job the
// provider is waiting on.
type BackendObservation struct {
	// PendingJobID is the LocalAI job for the install or delete in progress.
	// +optional
	PendingJobID string `json:"pendingJobID,omitempty"`
}

// BackendSpec is the desired state of a Backend.
type BackendSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              BackendParameters `json:"forProvider"`
}

// BackendStatus is the observed state of a Backend.
type BackendStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 BackendObservation `json:"atProvider,omitempty"`
}

// A Backend is a LocalAI backend installed from a gallery. Deleting the
// resource uninstalls the backend, which models that use it cannot run without.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Backend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              BackendSpec   `json:"spec"`
	Status            BackendStatus `json:"status,omitempty"`
}

// BackendList contains a list of Backends.
// +kubebuilder:object:root=true
type BackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backend `json:"items"`
}

// GetItems returns the Backends as managed resources.
func (l *BackendList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
