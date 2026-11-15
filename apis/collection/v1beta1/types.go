package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CollectionSource is a URL a collection follows.
type CollectionSource struct {
	// URL is the page or file the collection follows.
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// UpdateIntervalMinutes is how often LocalAI reads the URL again. Omit it
	// to keep LocalAI's default, which is not compared on drift.
	// +optional
	UpdateIntervalMinutes int `json:"updateIntervalMinutes,omitempty"`
}

// CollectionParameters are the desired settings of a LocalAI knowledge-base
// collection.
type CollectionParameters struct {
	// Name is the collection's name in LocalAI.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Sources are the URLs the collection follows. The provider keeps exactly
	// these: URLs not listed here are stopped on the next reconcile.
	// +optional
	Sources []CollectionSource `json:"sources,omitempty"`
}

// CollectionObservation is the state LocalAI reports for the collection.
type CollectionObservation struct {
	// Name is the collection's name as LocalAI reports it.
	Name string `json:"name,omitempty"`
}

// CollectionSpec is the desired state of a Collection.
type CollectionSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              CollectionParameters `json:"forProvider"`
}

// CollectionStatus is the observed state of a Collection.
type CollectionStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 CollectionObservation `json:"atProvider,omitempty"`
}

// A Collection is a LocalAI knowledge-base collection that follows a set of
// URLs. Deleting it stops the URLs it follows; LocalAI has no endpoint that
// deletes a collection, so its entries remain.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
type Collection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CollectionSpec   `json:"spec"`
	Status            CollectionStatus `json:"status,omitempty"`
}

// CollectionList contains a list of Collections.
// +kubebuilder:object:root=true
type CollectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Collection `json:"items"`
}

// GetItems returns the Collections as managed resources.
func (l *CollectionList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
