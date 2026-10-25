package agent

import (
	"context"
	"strings"

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
	agentv1alpha1 "github.com/rossigee/provider-localai/apis/agent/v1alpha1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

const (
	errNotAgent          = "managed resource is not an Agent"
	errGetProviderConfig = "cannot get ProviderConfig"
	errProviderNotReady  = "provider is not ready"
	errGetAgent          = "cannot get LocalAI agent"
	errCreateAgent       = "cannot create LocalAI agent"
	errUpdateAgent       = "cannot update LocalAI agent"
	errDeleteAgent       = "cannot delete LocalAI agent"

	controllerName = "agents.agent.localai.crossplane.io"
)

func getProviderConfig(ctx context.Context, kube client.Client, cr *agentv1alpha1.Agent) (*v1beta1.ProviderConfig, error) {
	pcName := cr.GetProviderConfigReference().Name
	pc := &v1beta1.ProviderConfig{}
	if err := kube.Get(ctx, client.ObjectKey{Name: pcName, Namespace: "crossplane-system"}, pc); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}
	return pc, nil
}

// Setup registers the Agent controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(agentv1alpha1.SchemeGroupVersion.WithKind("Agent")),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithLogger(o.Logger.WithValues("controller", "Agent")),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&agentv1alpha1.Agent{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*agentv1alpha1.Agent)
	if !ok {
		return nil, errors.New(errNotAgent)
	}
	pc, err := getProviderConfig(ctx, c.kube, cr)
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

	apiKey, err := resolveAgentAPIKey(ctx, c.kube, cr)
	if err != nil {
		return nil, err
	}
	return &external{client: laic, apiKey: apiKey}, nil
}

// resolveAgentAPIKey reads the key the agent uses to call LocalAI's own API.
func resolveAgentAPIKey(ctx context.Context, kube client.Client, cr *agentv1alpha1.Agent) (string, error) {
	ref := cr.Spec.ForProvider.APIKeySecretRef
	if ref == nil {
		return "", nil
	}
	key := ref.Key
	if key == "" {
		key = "token"
	}
	secret := &corev1.Secret{}
	if err := kube.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, secret); err != nil {
		return "", errors.Wrapf(err, "cannot get agent API key secret %s/%s", ref.Namespace, ref.Name)
	}
	raw, ok := secret.Data[key]
	if !ok {
		return "", errors.Errorf("key %q not found in secret %s/%s", key, ref.Namespace, ref.Name)
	}
	return strings.TrimSpace(string(raw)), nil
}

type external struct {
	client *laiclient.Client
	apiKey string
}

func (e *external) Disconnect(_ context.Context) error { return nil }

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*agentv1alpha1.Agent)
	a, err := e.client.GetAgent(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAgent)
	}
	cr.Status.AtProvider.Name = a.Name
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: agentUpToDate(cr, a),
	}, nil
}

// agentUpToDate compares the desired agent against what LocalAI reports, so
// edits to any field are pushed rather than only model changes.
func agentUpToDate(cr *agentv1alpha1.Agent, a *laiclient.Agent) bool {
	p := cr.Spec.ForProvider
	if a.Model != p.Model || a.Description != p.Description || a.SystemPrompt != p.SystemPrompt {
		return false
	}
	// Skills are deliberately not compared. LocalAI builds whose agent API has
	// no selected_skills field drop it on write, so comparing it would report
	// drift forever and rewrite the agent on every poll.
	if !equalStrings(a.AllowedTools, p.AllowedTools) || !equalStrings(a.ExcludedTools, p.ExcludedTools) {
		return false
	}
	if p.EnableSkills != nil && (a.EnableSkills == nil || *a.EnableSkills != *p.EnableSkills) {
		return false
	}
	want := make([]laiclient.MCPServer, 0, len(p.MCPServers))
	for _, s := range p.MCPServers {
		want = append(want, laiclient.MCPServer{URL: s.URL, Token: s.Token})
	}
	if len(a.MCPServers) != len(want) {
		return false
	}
	for i := range want {
		if a.MCPServers[i].URL != want[i].URL || a.MCPServers[i].Token != want[i].Token {
			return false
		}
	}
	return true
}

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

func agentFromCR(cr *agentv1alpha1.Agent, apiKey string) laiclient.Agent {
	p := cr.Spec.ForProvider
	out := laiclient.Agent{
		Name:           p.Name,
		Model:          p.Model,
		Description:    p.Description,
		SystemPrompt:   p.SystemPrompt,
		SelectedSkills: p.Skills,
		EnableSkills:   p.EnableSkills,
		AllowedTools:   p.AllowedTools,
		ExcludedTools:  p.ExcludedTools,
		APIKey:         apiKey,
	}
	for _, s := range p.MCPServers {
		out.MCPServers = append(out.MCPServers, laiclient.MCPServer{
			URL:   s.URL,
			Token: s.Token,
		})
	}
	return out
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*agentv1alpha1.Agent)
	_, err := e.client.CreateAgent(ctx, agentFromCR(cr, e.apiKey))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgent)
	}
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*agentv1alpha1.Agent)
	_, err := e.client.UpdateAgent(ctx, cr.Spec.ForProvider.Name, agentFromCR(cr, e.apiKey))
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAgent)
	}
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*agentv1alpha1.Agent)
	if err := e.client.DeleteAgent(ctx, cr.Spec.ForProvider.Name); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAgent)
	}
	return managed.ExternalDelete{}, nil
}
