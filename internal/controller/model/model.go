package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errGalleryAndConfig = "spec.forProvider.gallery and spec.forProvider.config are mutually exclusive"
	errGalleryJob       = "LocalAI gallery install failed: %s"

	errNotModel         = "managed resource is not a Model"
	errGetModel         = "cannot list LocalAI models"
	errParseConfig      = "cannot parse model config"
	errCreateModel      = "cannot create LocalAI model"
	errUpdateModel      = "cannot update LocalAI model"
	errDeleteModel      = "cannot delete LocalAI model"
	errProviderNotReady = "provider is not ready"

	controllerName = "models.model.localai.m.crossplane.io"

	// pendingImportTimeout bounds how long Observe waits for an asynchronous
	// import to list the model before it submits the import again.
	pendingImportTimeout = 10 * time.Minute
)

// ImportModelResponse is the response envelope from POST /models/import.
type ImportModelResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	UUID    string `json:"uuid,omitempty"`
}

func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Model"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(modelv1beta1.SchemeGroupVersion.WithKind("Model")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).Named(controllerName).WithOptions(o.ForControllerRuntime()).For(&modelv1beta1.Model{}).Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*modelv1beta1.Model)
	if !ok {
		return nil, errors.New(errNotModel)
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
	rawClient, err := clients.NewLocalAIHTTPClient(ctx, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI HTTP client")
	}
	return &external{client: laic, cfg: cfg, httpClient: rawClient, allowedHosts: pc.Spec.AllowedModelUpstreams}, nil
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

type external struct {
	client     *laiclient.Client
	cfg        laiclient.Config
	httpClient *http.Client
	// allowedHosts is the ProviderConfig's spec.allowedModelUpstreams.
	allowedHosts []string
}

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
			cr.Status.AtProvider.PendingImportUUID = ""
			cr.Status.AtProvider.PendingImportSince = ""
			clients.RecordFirstObservation(&cr.Status.AtProvider.Ownership, cr.Status.AtProvider.Name)
			cr.Status.AtProvider.Name = m.Name
			cr.Status.SetConditions(xpv1.Available())
			if cr.Spec.ForProvider.Gallery != nil {
				current, err := e.stateCurrent(ctx, cr)
				if err != nil {
					return managed.ExternalObservation{}, errors.Wrap(err, errGetModel)
				}
				return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: current}, nil
			}
			observed := cr.Status.AtProvider.ConfigHash
			if observed == "" {
				// Adopt the existing external model as the baseline for
				// drift detection without overwriting its stored
				// configuration on first contact.
				cr.Status.AtProvider.ConfigHash = hash
				observed = hash
			}
			current := observed == hash
			if current {
				current, err = e.stateCurrent(ctx, cr)
				if err != nil {
					return managed.ExternalObservation{}, errors.Wrap(err, errGetModel)
				}
			}
			return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: current}, nil
		}
	}
	// The model is not listed. For a gallery install, a failed job is
	// reported and cleared so the next reconcile installs it again.
	if cr.Spec.ForProvider.Gallery != nil && cr.Status.AtProvider.PendingImportUUID != "" {
		job, err := e.client.GetModelJob(ctx, cr.Status.AtProvider.PendingImportUUID)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errGetModel)
		}
		if job.Error != "" {
			cr.Status.AtProvider.PendingImportUUID = ""
			cr.Status.AtProvider.PendingImportSince = ""
			return managed.ExternalObservation{}, errors.Errorf(errGalleryJob, job.Error)
		}
	}
	// The model is not listed. If an import job is still inside its timeout,
	// report it as existing and current so the reconciler does not post the
	// import again. Once the model appears, the branch above records it.
	if cr.Status.AtProvider.PendingImportUUID != "" && !pendingImportExpired(cr.Status.AtProvider.PendingImportSince, time.Now()) {
		return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: true}, nil
	}
	cr.Status.AtProvider.PendingImportUUID = ""
	cr.Status.AtProvider.PendingImportSince = ""
	return managed.ExternalObservation{ResourceExists: false}, nil
}

// installFromGallery starts a gallery install under the Model's name and
// records its job for Observe to follow.
func (e *external) installFromGallery(ctx context.Context, cr *modelv1beta1.Model) (managed.ExternalCreation, error) {
	if cr.Spec.ForProvider.Config != "" {
		return managed.ExternalCreation{}, errors.New(errGalleryAndConfig)
	}
	g := cr.Spec.ForProvider.Gallery
	resp, err := e.client.ApplyModelFromGallery(ctx, laiclient.GalleryApplyRequest{
		ID:      g.ID,
		Variant: g.Variant,
		Name:    cr.Spec.ForProvider.Name,
	})
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateModel)
	}
	cr.Status.AtProvider.PendingImportUUID = resp.ID
	cr.Status.AtProvider.PendingImportSince = time.Now().UTC().Format(time.RFC3339)
	cr.Status.AtProvider.Name = cr.Spec.ForProvider.Name
	cr.Status.AtProvider.Ownership = clients.OwnershipCreated
	return managed.ExternalCreation{}, nil
}

// stateCurrent reports whether the enabled and pinned flags LocalAI reports
// match the spec. A flag the spec does not set is not compared. A model
// LocalAI does not list yet is not current, so the next reconcile applies
// the flags once it appears.
func (e *external) stateCurrent(ctx context.Context, cr *modelv1beta1.Model) (bool, error) {
	p := cr.Spec.ForProvider
	if p.Enabled == nil && p.Pinned == nil {
		return true, nil
	}
	caps, err := e.client.ListModelCapabilities(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range caps {
		if c.ID != p.Name {
			continue
		}
		// enabled is !disabled, so the flags disagree when the spec's
		// enabled value equals the reported disabled value.
		if p.Enabled != nil && *p.Enabled == c.Disabled {
			return false, nil
		}
		if p.Pinned != nil && *p.Pinned != c.Pinned {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

// syncState applies the managed flags that differ from LocalAI. It runs after
// the config import, because an import rewrites the model's file. A model
// LocalAI does not list yet is left for the next reconcile.
func (e *external) syncState(ctx context.Context, cr *modelv1beta1.Model) error {
	p := cr.Spec.ForProvider
	if p.Enabled == nil && p.Pinned == nil {
		return nil
	}
	caps, err := e.client.ListModelCapabilities(ctx)
	if err != nil {
		return err
	}
	for _, c := range caps {
		if c.ID != p.Name {
			continue
		}
		if p.Enabled != nil && *p.Enabled == c.Disabled {
			if err := e.client.SetModelEnabled(ctx, p.Name, *p.Enabled); err != nil {
				return err
			}
		}
		if p.Pinned != nil && *p.Pinned != c.Pinned {
			if err := e.client.SetModelPinned(ctx, p.Name, *p.Pinned); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// pendingImportExpired reports whether a pending import submitted at since is
// past pendingImportTimeout. An unparseable timestamp counts as expired, so
// the import is submitted again rather than waited on indefinitely.
func pendingImportExpired(since string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return true
	}
	return now.Sub(t) >= pendingImportTimeout
}

// importModel posts the desired configuration to /models/import. A response
// with a job envelope (UUID set) means the config referenced remote assets
// and is installing asynchronously; Observe reports existence once it lands.
func (e *external) importModel(ctx context.Context, cr *modelv1beta1.Model) error {
	desired, err := desiredModelConfig(cr)
	if err != nil {
		return errors.Wrap(err, errParseConfig)
	}
	if err := checkConfigURLs(desired, e.allowedHosts); err != nil {
		return err
	}
	var res ImportModelResponse
	if err := e.do(ctx, http.MethodPost, "/models/import", desired, &res); err != nil {
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
	if res.UUID != "" {
		cr.Status.AtProvider.PendingImportUUID = res.UUID
		cr.Status.AtProvider.PendingImportSince = time.Now().UTC().Format(time.RFC3339)
	} else {
		cr.Status.AtProvider.PendingImportUUID = ""
		cr.Status.AtProvider.PendingImportSince = ""
	}
	return nil
}

// do makes an authenticated JSON request to the LocalAI API. It mirrors the
// unexported Client.do method on laiclient.Client, which is not accessible
// from this package because localai-client v0.1.5 does not export ImportModel
// or DeleteModel for models.
func (e *external) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return errors.Wrap(err, "cannot encode request body")
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(e.cfg.BaseURL, "/")+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return errors.Wrap(err, "cannot read response")
	}
	if closeErr != nil {
		return errors.Wrap(closeErr, "cannot close response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &laiclient.APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode: %w: %s", err, string(raw))
		}
	}
	return nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*modelv1beta1.Model)
	if cr.Spec.ForProvider.Gallery != nil {
		return e.installFromGallery(ctx, cr)
	}
	if err := e.importModel(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateModel)
	}
	if err := e.syncState(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateModel)
	}
	cr.Status.AtProvider.Name = cr.Spec.ForProvider.Name
	cr.Status.AtProvider.Ownership = clients.OwnershipCreated
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*modelv1beta1.Model)
	if cr.Spec.ForProvider.Gallery != nil {
		if cr.Spec.ForProvider.Config != "" {
			return managed.ExternalUpdate{}, errors.New(errGalleryAndConfig)
		}
		if err := e.syncState(ctx, cr); err != nil {
			return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateModel)
		}
		return managed.ExternalUpdate{}, nil
	}
	if err := e.importModel(ctx, cr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateModel)
	}
	if err := e.syncState(ctx, cr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateModel)
	}
	cr.Status.AtProvider.Name = cr.Spec.ForProvider.Name
	return managed.ExternalUpdate{}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*modelv1beta1.Model)
	if !clients.DeletesExternal(cr.Status.AtProvider.Ownership) {
		// The model was already in LocalAI and is not ours to remove.
		return managed.ExternalDelete{}, nil
	}
	// Deletion is asynchronous: LocalAI returns a job envelope and the model
	// leaves the list from /v1/models once it completes, which Observe sees.
	path := "/models/delete/" + url.PathEscape(cr.Spec.ForProvider.Name)
	if err := e.do(ctx, http.MethodPost, path, nil, nil); err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteModel)
	}
	return managed.ExternalDelete{}, nil
}
