package skill

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
	skillv1beta1 "github.com/rossigee/provider-localai/apis/skill/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotSkill    = "managed resource is not a Skill"
	errGetSkill    = "cannot get LocalAI skill"
	errCreateSkill = "cannot create LocalAI skill"
	errUpdateSkill = "cannot update LocalAI skill"
	errDeleteSkill = "cannot delete LocalAI skill"

	controllerName = "skills.skill.localai.m.crossplane.io"
)

// Setup registers the Skill controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Skill"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(skillv1beta1.SchemeGroupVersion.WithKind("Skill")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&skillv1beta1.Skill{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*skillv1beta1.Skill)
	if !ok {
		return nil, errors.New(errNotSkill)
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

// Observe looks the skill up by its layer and name.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*skillv1beta1.Skill)
	p := cr.Spec.ForProvider
	s, err := e.client.GetSkill(ctx, p.Layer, p.Name)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetSkill)
	}
	cr.Status.AtProvider.Layer = s.Layer
	cr.Status.AtProvider.Name = s.Name
	cr.Status.SetConditions(xpv1.Available())
	upToDate := s.Body == p.Content && s.Description == p.Description
	return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: upToDate}, nil
}

// Create saves the skill in its layer.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*skillv1beta1.Skill)
	p := cr.Spec.ForProvider
	if _, err := e.client.CreateSkill(ctx, laiclient.SkillWrite{
		Layer:       p.Layer,
		Name:        p.Name,
		Description: p.Description,
		Body:        p.Content,
	}); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateSkill)
	}
	return managed.ExternalCreation{}, nil
}

// Update replaces the skill's description and body.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*skillv1beta1.Skill)
	p := cr.Spec.ForProvider
	if _, err := e.client.UpdateSkill(ctx, p.Layer, p.Name, laiclient.SkillUpdate{
		Description: p.Description,
		Body:        p.Content,
	}); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateSkill)
	}
	return managed.ExternalUpdate{}, nil
}

// Delete removes the skill. A skill that is already gone is not an error.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*skillv1beta1.Skill)
	p := cr.Spec.ForProvider
	if err := e.client.DeleteSkill(ctx, p.Layer, p.Name); err != nil && !laiclient.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteSkill)
	}
	return managed.ExternalDelete{}, nil
}
