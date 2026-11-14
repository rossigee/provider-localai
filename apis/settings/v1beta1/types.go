package v1beta1

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SettingsParameters define the LocalAI runtime settings this resource applies.
type SettingsParameters struct {
	// Settings is a JSON object of LocalAI runtime settings, such as
	// {"watchdog_idle_timeout": "15m", "threads": 8}. It is posted to
	// POST /api/settings, which LocalAI merges into its saved settings, so only
	// the keys given here change. Only keys on the provider's allow-list are
	// accepted; security-sensitive keys such as api_keys are rejected.
	// +kubebuilder:validation:Required
	Settings string `json:"settings"`
}

// SettingsObservation is the observed state of the applied settings.
type SettingsObservation struct {
	// AppliedHash is the sha256 of the canonical settings last posted to
	// LocalAI. It changes when spec.forProvider.settings changes. The provider
	// does not read settings back from LocalAI, so settings changed directly in
	// LocalAI are not detected.
	AppliedHash string `json:"appliedHash,omitempty"`
}

type SettingsSpec struct {
	xpv1.ManagedResourceSpec `json:",inline"`
	ForProvider              SettingsParameters `json:"forProvider"`
}

type SettingsStatus struct {
	xpv1.ManagedResourceStatus `json:",inline"`
	AtProvider                 SettingsObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,localai}
type Settings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SettingsSpec   `json:"spec"`
	Status            SettingsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Settings `json:"items"`
}

// GetItems returns the Settings resources in the list.
func (l *SettingsList) GetItems() []resource.Managed {
	out := make([]resource.Managed, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, &l.Items[i])
	}
	return out
}
