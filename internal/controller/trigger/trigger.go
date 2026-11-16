package trigger

import (
	"context"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"
	triggerv1beta1 "github.com/rossigee/provider-localai/apis/trigger/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotTrigger    = "managed resource is not a Trigger"
	errGetTrigger    = "cannot get LocalAI trigger"
	errCreateTrigger = "cannot create LocalAI trigger"
	errUpdateTrigger = "cannot update LocalAI trigger"
	errDeleteTrigger = "cannot delete LocalAI trigger"

	controllerName = "triggers.trigger.localai.m.crossplane.io"
)

// Setup registers the Trigger controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Trigger"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(triggerv1beta1.SchemeGroupVersion.WithKind("Trigger")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&triggerv1beta1.Trigger{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*triggerv1beta1.Trigger)
	if !ok {
		return nil, errors.New(errNotTrigger)
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

// Observe looks the trigger up by its external name, the LocalAI ID recorded at
// creation. A trigger without an external name does not exist yet.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*triggerv1beta1.Trigger)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	t, err := e.client.GetTrigger(ctx, id)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetTrigger)
	}
	cr.Status.AtProvider.ID = t.ID
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: triggerUpToDate(cr, t),
	}, nil
}

// triggerUpToDate compares the fields the spec manages with what LocalAI
// reports. An unset enabled flag is LocalAI's default, enabled, and an unset
// daily cap is not compared.
func triggerUpToDate(cr *triggerv1beta1.Trigger, t *laiclient.Trigger) bool {
	p := cr.Spec.ForProvider
	if t.Name != p.Name || t.Prompt != p.Prompt || t.Kind != p.Kind ||
		t.Schedule != p.Schedule || t.TimeZone != p.TimeZone ||
		t.AgentID != p.AgentID || t.Model != p.Model {
		return false
	}
	if p.DailyCap != 0 && t.DailyCap != p.DailyCap {
		return false
	}
	want := true
	if p.Enabled != nil {
		want = *p.Enabled
	}
	return t.Enabled == want
}

// triggerWrite builds the body LocalAI expects from the spec.
func triggerWrite(cr *triggerv1beta1.Trigger) laiclient.TriggerWrite {
	p := cr.Spec.ForProvider
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	return laiclient.TriggerWrite{
		Name:     p.Name,
		Prompt:   p.Prompt,
		Kind:     p.Kind,
		Schedule: p.Schedule,
		TimeZone: p.TimeZone,
		DailyCap: p.DailyCap,
		Enabled:  enabled,
		AgentID:  p.AgentID,
		Model:    p.Model,
	}
}

// Create saves the trigger and records its LocalAI ID as the external name.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*triggerv1beta1.Trigger)
	t, err := e.client.CreateTrigger(ctx, triggerWrite(cr))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateTrigger)
	}
	meta.SetExternalName(cr, t.ID)
	cr.Status.AtProvider.ID = t.ID
	return managed.ExternalCreation{}, nil
}

// Update replaces the saved trigger. LocalAI's update replaces the whole
// trigger, so the full desired state is sent.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*triggerv1beta1.Trigger)
	if _, err := e.client.UpdateTrigger(ctx, meta.GetExternalName(cr), triggerWrite(cr)); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateTrigger)
	}
	return managed.ExternalUpdate{}, nil
}

// Delete removes the trigger. A trigger that is already gone is not an error.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*triggerv1beta1.Trigger)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalDelete{}, nil
	}
	if err := e.client.DeleteTrigger(ctx, id); err != nil && !laiclient.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteTrigger)
	}
	return managed.ExternalDelete{}, nil
}
