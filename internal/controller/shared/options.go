// Package shared holds reconciler settings common to every LocalAI controller.
package shared

import (
	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
)

// ReconcilerOptions returns the managed reconciler options that honour the
// provider's runtime flags: the logger for kind, the poll interval, and
// management policies when --enable-management-policies is set.
func ReconcilerOptions(o xpcontroller.Options, kind string) []managed.ReconcilerOption {
	opts := []managed.ReconcilerOption{
		managed.WithLogger(o.Logger.WithValues("controller", kind)),
		managed.WithPollInterval(o.PollInterval),
	}
	if o.Features != nil && o.Features.Enabled(feature.EnableBetaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}
	return opts
}
