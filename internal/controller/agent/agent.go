package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	errInlineMCPToken   = "spec.forProvider.mcpServers[%d].token is no longer supported: move the token into a Secret and set tokenSecretRef"

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

	creds, err := resolveCredentials(ctx, c.kube, cr)
	if err != nil {
		return nil, err
	}
	return &external{client: laic, creds: creds}, nil
}

// credentials are the secret values an agent is created with.
type credentials struct {
	// apiKey is the key the agent uses to call LocalAI's own API. Empty when
	// no apiKeySecretRef is set.
	apiKey string
	// mcpTokens are index-aligned with spec.forProvider.mcpServers. An entry is
	// empty when that server has no tokenSecretRef.
	mcpTokens []string
}

// hash digests the credential values. It is recorded in status so that a
// rotated Secret is detected without storing the values themselves.
func (c credentials) hash() string {
	h := sha256.New()
	h.Write([]byte(c.apiKey))
	for _, t := range c.mcpTokens {
		h.Write([]byte{0})
		h.Write([]byte(t))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// resolveCredentials reads every Secret the agent refers to.
func resolveCredentials(ctx context.Context, kube client.Client, cr *agentv1beta1.Agent) (credentials, error) {
	apiKey, err := resolveSecretValue(ctx, kube, cr.GetNamespace(), cr.Spec.ForProvider.APIKeySecretRef, "apiKeySecretRef")
	if err != nil {
		return credentials{}, err
	}
	creds := credentials{apiKey: apiKey, mcpTokens: make([]string, 0, len(cr.Spec.ForProvider.MCPServers))}
	for i, s := range cr.Spec.ForProvider.MCPServers {
		if s.Token != "" {
			return credentials{}, errors.Errorf(errInlineMCPToken, i)
		}
		t, err := resolveSecretValue(ctx, kube, cr.GetNamespace(), s.TokenSecretRef, "mcpServers.tokenSecretRef")
		if err != nil {
			return credentials{}, err
		}
		creds.mcpTokens = append(creds.mcpTokens, t)
	}
	return creds, nil
}

// resolveSecretValue reads one key from a Secret that must live in the
// Agent's namespace. A nil ref yields an empty value.
func resolveSecretValue(ctx context.Context, kube client.Client, agentNamespace string, ref *xpv1.SecretKeySelector, field string) (string, error) {
	if ref == nil {
		return "", nil
	}
	// The Secret must live beside the Agent. Otherwise any tenant that can create
	// an Agent could copy a value from a namespace it cannot read.
	if ref.Namespace != agentNamespace {
		return "", errors.Errorf("%s must reference a Secret in the Agent's namespace %q; got %q", field, agentNamespace, ref.Namespace)
	}
	key := ref.Key
	if key == "" {
		key = "token"
	}
	secret := &corev1.Secret{}
	if err := kube.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, secret); err != nil {
		return "", errors.Wrapf(err, "cannot get %s secret %s/%s", field, ref.Namespace, ref.Name)
	}
	raw, ok := secret.Data[key]
	if !ok {
		return "", errors.Errorf("key %q not found in secret %s/%s", key, ref.Namespace, ref.Name)
	}
	return strings.TrimSpace(string(raw)), nil
}

type external struct {
	client *laiclient.Client
	creds  credentials
}

func (e *external) Disconnect(_ context.Context) error { return nil }

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*agentv1beta1.Agent)
	a, err := e.client.GetAgent(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetAgent)
	}
	clients.RecordFirstObservation(&cr.Status.AtProvider.Ownership, cr.Status.AtProvider.Name)
	cr.Status.AtProvider.Name = a.Name
	cr.Status.SetConditions(xpv1.Available())

	digest := e.creds.hash()
	if cr.Status.AtProvider.CredentialsHash == "" {
		// First observation: record the baseline without rewriting the agent,
		// so existing agents are adopted rather than overwritten.
		cr.Status.AtProvider.CredentialsHash = digest
	}
	credsCurrent := cr.Status.AtProvider.CredentialsHash == digest
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: agentUpToDate(cr, a) && credsCurrent,
	}, nil
}

// agentUpToDate compares the desired agent against what LocalAI reports, so
// edits to any field are pushed rather than only model changes. Credentials
// are compared through their digest in status, not against LocalAI, because
// LocalAI may not return them.
func agentUpToDate(cr *agentv1beta1.Agent, a *laiclient.Agent) bool {
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
	if p.EnableKB != nil && (a.EnableKB == nil || *a.EnableKB != *p.EnableKB) {
		return false
	}
	if p.KbAsTools != nil && (a.KbAsTools == nil || *a.KbAsTools != *p.KbAsTools) {
		return false
	}
	if p.LongTermMemory != nil && (a.LongTermMemory == nil || *a.LongTermMemory != *p.LongTermMemory) {
		return false
	}
	if len(a.MCPServers) != len(p.MCPServers) {
		return false
	}
	for i, s := range p.MCPServers {
		if a.MCPServers[i].URL != s.URL {
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

func agentFromCR(cr *agentv1beta1.Agent, creds credentials) laiclient.Agent {
	p := cr.Spec.ForProvider
	out := laiclient.Agent{
		Name:           p.Name,
		Model:          p.Model,
		Description:    p.Description,
		SystemPrompt:   p.SystemPrompt,
		SelectedSkills: p.Skills,
		EnableSkills:   p.EnableSkills,
		EnableKB:       p.EnableKB,
		KbAsTools:      p.KbAsTools,
		LongTermMemory: p.LongTermMemory,
		AllowedTools:   p.AllowedTools,
		ExcludedTools:  p.ExcludedTools,
		APIKey:         creds.apiKey,
	}
	for i, s := range p.MCPServers {
		out.MCPServers = append(out.MCPServers, laiclient.MCPServer{
			URL:   s.URL,
			Token: creds.mcpTokens[i],
		})
	}
	return out
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*agentv1beta1.Agent)
	_, err := e.client.CreateAgent(ctx, agentFromCR(cr, e.creds))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgent)
	}
	cr.Status.AtProvider.CredentialsHash = e.creds.hash()
	cr.Status.AtProvider.Ownership = clients.OwnershipCreated
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*agentv1beta1.Agent)
	desired := agentFromCR(cr, e.creds)
	// Preserve KB/memory settings that are not managed by the CR. LocalAI
	// treats omitted fields as "reset to default", so we fetch the current
	// config and carry over any KB/memory values the CR does not set.
	current, err := e.client.GetAgent(ctx, cr.Spec.ForProvider.Name)
	if err != nil && !laiclient.IsNotFound(err) {
		// Updating without the current settings would reset them.
		return managed.ExternalUpdate{}, errors.Wrap(err, errGetAgent)
	}
	if err == nil {
		if desired.EnableKB == nil {
			desired.EnableKB = current.EnableKB
		}
		if desired.KbAsTools == nil {
			desired.KbAsTools = current.KbAsTools
		}
		if desired.LongTermMemory == nil {
			desired.LongTermMemory = current.LongTermMemory
		}
	}
	_, err = e.client.UpdateAgent(ctx, cr.Spec.ForProvider.Name, desired)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateAgent)
	}
	cr.Status.AtProvider.CredentialsHash = e.creds.hash()
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*agentv1beta1.Agent)
	if !clients.DeletesExternal(cr.Status.AtProvider.Ownership) {
		// The agent was already in LocalAI and is not ours to remove.
		return managed.ExternalDelete{}, nil
	}
	if err := e.client.DeleteAgent(ctx, cr.Spec.ForProvider.Name); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteAgent)
	}
	return managed.ExternalDelete{}, nil
}
