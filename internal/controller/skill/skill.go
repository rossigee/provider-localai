package skill

import (
	"context"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	laiclient "github.com/rossigee/localai-client/client"
	skillv1alpha1 "github.com/rossigee/provider-localai/apis/skill/v1alpha1"
)

const (
	errNotSkill    = "managed resource is not a Skill"
	errGetSkill    = "cannot get LocalAI skill"
	errCreateSkill = "cannot create LocalAI skill"
	errUpdateSkill = "cannot update LocalAI skill"
	errDeleteSkill = "cannot delete LocalAI skill"

	controllerName = "skills.skill.localai.crossplane.io"
)

func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(skillv1alpha1.SchemeGroupVersion.WithKind("Skill")),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithLogger(o.Logger.WithValues("controller", "Skill")),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&skillv1alpha1.Skill{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*skillv1alpha1.Skill)
	if !ok {
		return nil, errors.New(errNotSkill)
	}
	_ = cr
	cfg := laiclient.Config{}
	return &external{client: laiclient.New(cfg)}, nil
}

type external struct{ client *laiclient.Client }

func (e *external) Disconnect(_ context.Context) error { return nil }

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*skillv1alpha1.Skill)
	s, err := e.client.GetSkill(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	cr.Status.AtProvider.Name = s.Name
	return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*skillv1alpha1.Skill)
	_, err := e.client.CreateSkill(ctx, laiclient.Skill{
		Name:        cr.Spec.ForProvider.Name,
		Description: cr.Spec.ForProvider.Description,
	})
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateSkill)
	}
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*skillv1alpha1.Skill)
	_, err := e.client.UpdateSkill(ctx, cr.Spec.ForProvider.Name, laiclient.Skill{
		Name:        cr.Spec.ForProvider.Name,
		Description: cr.Spec.ForProvider.Description,
	})
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateSkill)
	}
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*skillv1alpha1.Skill)
	if err := e.client.DeleteSkill(ctx, cr.Spec.ForProvider.Name); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteSkill)
	}
	return managed.ExternalDelete{}, nil
}
