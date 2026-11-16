package agenttask

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
	taskv1beta1 "github.com/rossigee/provider-localai/apis/agenttask/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotAgentTask    = "managed resource is not an AgentTask"
	errGetAgentTask    = "cannot get LocalAI agent task"
	errCreateAgentTask = "cannot create LocalAI agent task"
	errUpdateAgentTask = "cannot update LocalAI agent task"
	errDeleteAgentTask = "cannot delete LocalAI agent task"

	controllerName = "agenttasks.agenttask.localai.m.crossplane.io"
)

// Setup registers the AgentTask controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "AgentTask"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(taskv1beta1.SchemeGroupVersion.WithKind("AgentTask")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&taskv1beta1.AgentTask{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*taskv1beta1.AgentTask)
	if !ok {
		return nil, errors.New(errNotAgentTask)
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

// Observe looks the task up by its external name, the LocalAI ID recorded at
// creation. A task without an external name does not exist yet.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*taskv1beta1.AgentTask)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	t, err := e.client.GetAgentTask(ctx, id)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAgentTask)
	}
	cr.Status.AtProvider.ID = t.ID
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: taskUpToDate(cr, t),
	}, nil
}

// taskUpToDate compares every field the spec manages with what LocalAI reports.
// An unset enabled flag is LocalAI's default, enabled.
func taskUpToDate(cr *taskv1beta1.AgentTask, t *laiclient.AgentTask) bool {
	p := cr.Spec.ForProvider
	if t.Name != p.Name || t.Description != p.Description || t.Model != p.Model ||
		t.AgentID != p.AgentID || t.Prompt != p.Prompt {
		return false
	}
	want := true
	if p.Enabled != nil {
		want = *p.Enabled
	}
	got := true
	if t.Enabled != nil {
		got = *t.Enabled
	}
	return want == got
}

// taskWrite builds the body LocalAI expects from the spec.
func taskWrite(cr *taskv1beta1.AgentTask) laiclient.AgentTaskWrite {
	p := cr.Spec.ForProvider
	return laiclient.AgentTaskWrite{
		Name:        p.Name,
		Description: p.Description,
		Model:       p.Model,
		AgentID:     p.AgentID,
		Prompt:      p.Prompt,
		Enabled:     p.Enabled,
	}
}

// Create saves the task and records its LocalAI ID as the external name.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*taskv1beta1.AgentTask)
	id, err := e.client.CreateAgentTask(ctx, taskWrite(cr))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgentTask)
	}
	meta.SetExternalName(cr, id)
	cr.Status.AtProvider.ID = id
	return managed.ExternalCreation{}, nil
}

// Update replaces the saved task. LocalAI's update replaces every field, so the
// full desired state is sent.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*taskv1beta1.AgentTask)
	if err := e.client.UpdateAgentTask(ctx, meta.GetExternalName(cr), taskWrite(cr)); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAgentTask)
	}
	return managed.ExternalUpdate{}, nil
}

// Delete removes the saved task. A task that is already gone is not an error.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*taskv1beta1.AgentTask)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalDelete{}, nil
	}
	if err := e.client.DeleteAgentTask(ctx, id); err != nil && !laiclient.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAgentTask)
	}
	return managed.ExternalDelete{}, nil
}
