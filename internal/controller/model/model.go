package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"
	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

const (
	errNotModel         = "managed resource is not a Model"
	errGetModel         = "cannot list LocalAI models"
	errParseConfig      = "cannot parse model config"
	errCreateModel      = "cannot create LocalAI model"
	errUpdateModel      = "cannot update LocalAI model"
	errDeleteModel      = "cannot delete LocalAI model"
	errProviderNotReady = "provider is not ready"

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

// desiredModelConfig renders the configuration to POST to /models/import. The
// CR's name and backend are authoritative; the optional YAML config supplies
// everything else, and must not contradict them.
func desiredModelConfig(cr *modelv1beta1.Model) (map[string]any, error) {
	p := cr.Spec.ForProvider
	out := map[string]any{"name": p.Name}
	if p.Backend != "" {
		out["backend"] = p.Backend
	}
	if strings.TrimSpace(p.Config) == "" {
		return out, nil
	}
	parsed := map[string]any{}
	if err := yaml.Unmarshal([]byte(p.Config), &parsed); err != nil {
		return nil, errors.Wrap(err, errParseConfig)
	}
	if n, ok := parsed["name"]; ok && n != p.Name {
		return nil, errors.Errorf("config name %v does not match spec name %q", n, p.Name)
	}
	if b, ok := parsed["backend"]; ok && p.Backend != "" && b != p.Backend {
		return nil, errors.Errorf("config backend %v does not match spec backend %q", b, p.Backend)
	}
	for k, v := range parsed {
		out[k] = v
	}
	out["name"] = p.Name
	if p.Backend != "" {
		out["backend"] = p.Backend
	}
	return out, nil
}

// modelConfigHash is the sha256 of the canonical JSON of cfg. encoding/json
// marshals maps with sorted keys, so equal configurations hash equally.
func modelConfigHash(cfg map[string]any) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", errors.Wrap(err, "cannot encode model config")
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type external struct{ client *laiclient.Client }

func (e *external) Disconnect(_ context.Context) error { return nil }

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*modelv1beta1.Model)
	desired, err := desiredModelConfig(cr)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	models, err := e.client.ListModels(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errGetModel)
	}
	hash, err := modelConfigHash(desired)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	for _, m := range models {
		if m.Name == cr.Spec.ForProvider.Name {
			cr.Status.AtProvider.Name = m.Name
			cr.Status.SetConditions(xpv1.Available())
			observed := cr.Status.AtProvider.ConfigHash
			if observed == "" {
				// Adopt the existing external model as the baseline for
				// drift detection without overwriting its stored
				// configuration on first contact. LocalAI has no endpoint
				// that returns a model's config, so the status hash is the
				// only way to know when the spec has moved.
				cr.Status.AtProvider.ConfigHash = hash
				return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
			}
			return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: observed == hash}, nil
		}
	}
	return managed.ExternalObservation{ResourceExists: false}, nil
}

// importModel posts the desired configuration to /models/import. A response
// with a job envelope (UUID set) means the config referenced remote assets
// and is installing asynchronously; Observe reports existence once it lands.
func (e *external) importModel(ctx context.Context, cr *modelv1beta1.Model) error {
	desired, err := desiredModelConfig(cr)
	if err != nil {
		return errors.Wrap(err, errParseConfig)
	}
	res, err := e.client.ImportModel(ctx, desired)
	if err != nil {
		return err
	}
	if !res.Success && res.UUID == "" {
		return errors.Errorf("localai rejected model %q: %s", cr.Spec.ForProvider.Name, res.Message)
	}
	hash, err := modelConfigHash(desired)
	if err != nil {
		return err
	}
	cr.Status.AtProvider.ConfigHash = hash
	return nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*modelv1beta1.Model)
	if err := e.importModel(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateModel)
	}
	cr.Status.AtProvider.Name = cr.Spec.ForProvider.Name
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*modelv1beta1.Model)
	if err := e.importModel(ctx, cr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateModel)
	}
	cr.Status.AtProvider.Name = cr.Spec.ForProvider.Name
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*modelv1beta1.Model)
	// Deletion is asynchronous: LocalAI returns a job envelope and the model
	// leaves the list from /v1/models once it completes, which Observe sees.
	if _, err := e.client.DeleteModel(ctx, cr.Spec.ForProvider.Name); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteModel)
	}
	return managed.ExternalDelete{}, nil
}
