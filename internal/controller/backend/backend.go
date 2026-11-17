package backend

import (
	"context"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"
	backendv1beta1 "github.com/rossigee/provider-localai/apis/backend/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotBackend     = "managed resource is not a Backend"
	errGetBackend     = "cannot get LocalAI backend"
	errInstallBackend = "cannot install LocalAI backend"
	errDeleteBackend  = "cannot delete LocalAI backend"
	errBackendJob     = "LocalAI backend job failed: %s"

	controllerName = "backends.backend.localai.m.crossplane.io"
)

// Setup registers the Backend controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Backend"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(backendv1beta1.SchemeGroupVersion.WithKind("Backend")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&backendv1beta1.Backend{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*backendv1beta1.Backend)
	if !ok {
		return nil, errors.New(errNotBackend)
	}
	pc, err := clients.GetProviderConfig(ctx, c.kube, cr.GetNamespace(), cr.GetProviderConfigReference())
	if err != nil {
		return nil, err
	}
	if pc.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionTrue {
		return nil, errors.New("provider is not ready")
	}
	cfg, err := clients.GetLocalAIConfig(ctx, pc, c.kube)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI client config")
	}
	laic, err := laiclient.New(ctx, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI client")
	}
	return &external{client: laic}, nil
}

type external struct{ client *laiclient.Client }

func (e *external) Disconnect(_ context.Context) error { return nil }

// Observe reports whether the backend is installed. While an install or delete
// job is running, it reports the backend as present and current, so the
// reconciler waits for the job rather than starting another.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*backendv1beta1.Backend)
	if job := cr.Status.AtProvider.PendingJobID; job != "" {
		st, err := e.client.GetBackendJob(ctx, job)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errGetBackend)
		}
		if st.Error != "" {
			cr.Status.AtProvider.PendingJobID = ""
			return managed.ExternalObservation{}, errors.Errorf(errBackendJob, st.Error)
		}
		if !st.Processed {
			return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
		}
		cr.Status.AtProvider.PendingJobID = ""
	}
	entries, err := e.client.ListBackends(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errGetBackend)
	}
	installed := false
	for _, b := range entries {
		if b.Name == cr.Spec.ForProvider.Name && b.Installed {
			installed = true
			break
		}
	}
	if !installed {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
}

// Create starts the install and records its job.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*backendv1beta1.Backend)
	job, err := e.client.InstallBackend(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errInstallBackend)
	}
	cr.Status.AtProvider.PendingJobID = job
	return managed.ExternalCreation{}, nil
}

// Update has nothing to change: a backend is named by its gallery entry alone.
func (e *external) Update(_ context.Context, _ resource.Managed) (managed.ExternalUpdate, error) {
	return managed.ExternalUpdate{}, nil
}

// Delete starts the uninstall and records its job. While a job is pending, it
// does nothing, so the uninstall is requested once.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*backendv1beta1.Backend)
	if cr.Status.AtProvider.PendingJobID != "" {
		return managed.ExternalDelete{}, nil
	}
	job, err := e.client.DeleteBackend(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalDelete{}, nil
		}
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteBackend)
	}
	cr.Status.AtProvider.PendingJobID = job
	return managed.ExternalDelete{}, nil
}
