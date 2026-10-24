package agent

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
	if err := kube.Get(ctx, client.ObjectKey{Name: pcName}, pc); err != nil {
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
	return &external{client: laiclient.New(cfg)}, nil
}

type external struct{ client *laiclient.Client }

func (e *external) Disconnect(_ context.Context) error { return nil }

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*agentv1alpha1.Agent)
	a, err := e.client.GetAgent(ctx, cr.Spec.ForProvider.Name)
	if err != nil {
		// not found -> not exists
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	cr.Status.AtProvider.Name = a.Name
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: a.Model == cr.Spec.ForProvider.Model,
	}, nil
}

func agentFromCR(cr *agentv1alpha1.Agent) laiclient.Agent {
	p := cr.Spec.ForProvider
	out := laiclient.Agent{
		Name:          p.Name,
		Model:         p.Model,
		Description:   p.Description,
		SystemPrompt:  p.SystemPrompt,
		Skills:        p.Skills,
		EnableSkills:  p.EnableSkills,
		AllowedTools:  p.AllowedTools,
		ExcludedTools: p.ExcludedTools,
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
	_, err := e.client.CreateAgent(ctx, agentFromCR(cr))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateAgent)
	}
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*agentv1alpha1.Agent)
	_, err := e.client.UpdateAgent(ctx, cr.Spec.ForProvider.Name, agentFromCR(cr))
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
