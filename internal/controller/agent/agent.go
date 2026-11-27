package agent

import (
	"context"
	stderrors "errors"
	"net/http"

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
	agentv1beta1 "github.com/rossigee/provider-localai/apis/agent/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotAgent         = "managed resource is not an Agent"
	errProviderNotReady = "provider is not ready"
	errGetAgent         = "cannot get LocalAI agent"
	errCreateAgent      = "cannot create LocalAI agent"
	errUpdateAgent      = "cannot update LocalAI agent"
	errDeleteAgent      = "cannot delete LocalAI agent"

	controllerName = "agents.agent.localai.m.crossplane.io"
)

// Setup registers the Agent controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Agent"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(agentv1beta1.SchemeGroupVersion.WithKind("Agent")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&agentv1beta1.Agent{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*agentv1beta1.Agent)
	if !ok {
		return nil, errors.New(errNotAgent)
	}
	pc, err := clients.GetProviderConfig(ctx, c.kube, cr.GetNamespace(), cr.GetProviderConfigReference())
	if err != nil {
		return nil, err
	}
	if pc.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionTrue {
		return nil, errors.New(errProviderNotReady)
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

type external struct {
	client *laiclient.Client
}

func (e *external) Disconnect(_ context.Context) error { return nil }

// Observe looks the agent up by its external name, which is the LocalAI ID
// recorded at creation. An agent without an external name does not exist yet.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*agentv1beta1.Agent)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	a, err := e.client.GetAgent(ctx, id, cr.Spec.ForProvider.Owner)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAgent)
	}
	cr.Status.AtProvider.ID = a.ID
	cr.Status.AtProvider.Owner = a.Owner
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: agentUpToDate(cr, a),
	}, nil
}

// agentUpToDate compares every desired field with what LocalAI reports, so an
// edit to any field is pushed on the next reconcile.
func agentUpToDate(cr *agentv1beta1.Agent, a *laiclient.Agent) bool {
	p := cr.Spec.ForProvider
	if a.Name != p.Name || a.Description != p.Description || a.Instructions != p.Instructions || a.Model != p.Model {
		return false
	}
	if p.Owner != "" && a.Owner != p.Owner {
		return false
	}
	if networkOf(a.Network) != networkOf(p.Network) {
		return false
	}
	if !equalStrings(a.Skills, p.Skills) || !equalStrings(a.Collections, p.Collections) ||
		!equalStrings(a.MCPServers, p.MCPServers) || !equalStrings(a.Addons, p.Addons) {
		return false
	}
	return folderUpToDate(p.Folder, a.Folder)
}

// networkOf is the network preset LocalAI applies when none is set.
func networkOf(n string) string {
	if n == "" {
		return "off"
	}
	return n
}

// folderUpToDate compares the desired folder with the one LocalAI reports. The
// mode is compared only when the spec sets one, because LocalAI picks a default.
func folderUpToDate(want *agentv1beta1.AgentFolder, got *laiclient.AgentFolder) bool {
	if want == nil {
		return got == nil
	}
	if got == nil || got.Path != want.Path {
		return false
	}
	return want.Mode == "" || got.Mode == want.Mode
}

// equalStrings compares two lists as ordered lists, treating nil and empty as
// the same.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// agentWrite builds the body LocalAI expects from the spec.
func agentWrite(cr *agentv1beta1.Agent) laiclient.AgentWrite {
	p := cr.Spec.ForProvider
	out := laiclient.AgentWrite{
		Name:         p.Name,
		Description:  p.Description,
		Instructions: p.Instructions,
		Model:        p.Model,
		MCPServers:   p.MCPServers,
		Addons:       p.Addons,
		Collections:  p.Collections,
		Skills:       p.Skills,
		Network:      p.Network,
	}
	if p.Folder != nil {
		out.Folder = &laiclient.AgentFolderWrite{Path: p.Folder.Path, Mode: p.Folder.Mode}
	}
	return out
}

// Create saves the agent and records its LocalAI ID as the external name.
// Create saves the agent and records its LocalAI ID as the external name.
//
// An agent that already exists under this name (for example one created before
// the external name became the LocalAI ID) is adopted rather than failed: its ID
// is recorded, and the next reconcile updates it to the spec.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*agentv1beta1.Agent)
	a, err := e.client.CreateAgent(ctx, agentWrite(cr), cr.Spec.ForProvider.Owner)
	if err != nil {
		if !isNameConflict(err) {
			return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgent)
		}
		existing, lerr := e.findAgentByName(ctx, cr.Spec.ForProvider.Name, cr.Spec.ForProvider.Owner)
		if lerr != nil {
			return managed.ExternalCreation{}, errors.Wrap(lerr, errCreateAgent)
		}
		if existing == nil {
			return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgent)
		}
		meta.SetExternalName(cr, existing.ID)
		cr.Status.AtProvider.ID = existing.ID
		return managed.ExternalCreation{}, nil
	}
	meta.SetExternalName(cr, a.ID)
	cr.Status.AtProvider.ID = a.ID
	cr.Status.AtProvider.Owner = a.Owner
	return managed.ExternalCreation{}, nil
}

// isNameConflict reports whether LocalAI refused the create because an agent
// with the same name already exists.
func isNameConflict(err error) bool {
	var apiErr *laiclient.APIError
	return stderrors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// findAgentByName returns the saved agent with the given name, or nil.
func (e *external) findAgentByName(ctx context.Context, name, owner string) (*laiclient.Agent, error) {
	agents, err := e.client.ListAgents(ctx, owner)
	if err != nil {
		return nil, err
	}
	for i := range agents {
		if agents[i].Name == name {
			return &agents[i], nil
		}
	}
	return nil, nil
}

// Update replaces the saved agent. LocalAI's update replaces the whole agent,
// so the full desired state is sent.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*agentv1beta1.Agent)
	if _, err := e.client.UpdateAgent(ctx, meta.GetExternalName(cr), agentWrite(cr), cr.Spec.ForProvider.Owner); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAgent)
	}
	return managed.ExternalUpdate{}, nil
}

// Delete removes the saved agent. An agent that is already gone is not an
// error.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*agentv1beta1.Agent)
	id := meta.GetExternalName(cr)
	if id == "" {
		return managed.ExternalDelete{}, nil
	}
	if err := e.client.DeleteAgent(ctx, id, cr.Spec.ForProvider.Owner); err != nil && !laiclient.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAgent)
	}
	return managed.ExternalDelete{}, nil
}
