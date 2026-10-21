/*
Copyright 2024 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package deletecomplete provides the durable record a controller uses to tell
// the Crossplane managed reconciler that it has finished releasing the external
// resource it owns.
//
// The reconciler removes a managed resource's finalizer only once Observe
// reports the external resource as gone, and while Observe keeps reporting it
// present it re-runs Delete on every pass - it deliberately requeues to verify
// the external resource actually disappeared. A controller therefore has to be
// able to say "gone" at some point, or the object never terminates.
//
// Delete has no channel to report completion through, so controllers record it
// here and Observe reports ResourceExists: false once it is present.
package deletecomplete

import (
	"context"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Annotation records that Delete has finished releasing the external
	// resource this managed resource owns.
	Annotation = "keycloak.m.crossplane.io/delete-completed"

	// AnnotationValue is the only value that counts as complete.
	AnnotationValue = "true"

	// ErrMarkFailed is returned when completion cannot be persisted.
	ErrMarkFailed = "cannot record that the external resource was released"
)

// Done reports whether deletion has already been recorded as complete.
func Done(o client.Object) bool {
	return o.GetAnnotations()[Annotation] == AnnotationValue
}

// Mark records that the external resource has been released.
//
// The annotation is persisted with an explicit patch rather than assigned in
// memory, because the reconciler only writes back status - a metadata change
// made inside an external client is otherwise discarded with the reconcile.
//
// Callers must not call Mark when release failed, or the finalizer will be
// removed while the external resource still exists.
func Mark(ctx context.Context, kube client.Client, o client.Object) error {
	patch := client.MergeFrom(o.DeepCopyObject().(client.Object))

	annotations := o.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[Annotation] = AnnotationValue
	o.SetAnnotations(annotations)

	return errors.Wrap(kube.Patch(ctx, o, patch), ErrMarkFailed)
}
