package model

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
	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

const (
	errNotModel    = "managed resource is not a Model"
	errGetModel    = "cannot list LocalAI models"
	controllerName = "models.model.localai.m.crossplane.io"
)

func getProviderConfig(ctx context.Context, kube client.Client, cr *modelv1beta1.Model) (*v1beta1.ProviderConfig, error) {
	pcName := cr.GetProviderConfigReference().Name
	pc := &v1beta1.ProviderConfig{}
	if err := kube.Get(ctx, client.ObjectKey{Name: pcName, Namespace: "crossplane-system"}, pc); err != nil {
		return nil, errors.Wrap(err, "cannot get ProviderConfig")
	}
	return pc, nil
}

func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(modelv1beta1.SchemeGroupVersion.WithKind("Model")),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithLogger(o.Logger.WithValues("controller", "Model")),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	return ctrl.NewControllerManagedBy(mgr).Named(controllerName).WithOptions(o.ForControllerRuntime()).For(&modelv1beta1.Model{}).Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*modelv1beta1.Model)
	if !ok {
		return nil, errors.New(errNotModel)
	}
	pc, err := getProviderConfig(ctx, c.kube, cr)
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

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*modelv1beta1.Model)
	models, err := e.client.ListModels(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errGetModel)
	}
	for _, m := range models {
		if m.Name == cr.Spec.ForProvider.Name {
			cr.Status.AtProvider.Name = m.Name
			cr.Status.SetConditions(xpv1.Available())
			return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
		}
	}
	return managed.ExternalObservation{ResourceExists: false}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	return managed.ExternalDelete{}, nil
}
