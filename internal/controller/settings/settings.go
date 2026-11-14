package settings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	laiclient "github.com/rossigee/localai-client/client"
	settingsv1beta1 "github.com/rossigee/provider-localai/apis/settings/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotSettings      = "managed resource is not a Settings"
	errProviderNotReady = "provider is not ready"
	errPostSettings     = "cannot apply LocalAI settings"

	controllerName = "settings.settings.localai.m.crossplane.io"

	settingsPath = "/api/settings"
	// maxResponseBytes bounds how much of a LocalAI response is read.
	maxResponseBytes = 64 << 10
)

// allowedKeys are the runtime settings a Settings resource may set. Keys are
// excluded when they control authentication or cross-origin access (api_keys,
// csrf, cors, cors_allow_origins), make LocalAI fetch remote content (galleries,
// autoload_*), open a network (p2p_*, federated, mcp_connectors_allow_private),
// or enable tools (chats_*, client_jobs_allow_all_tools). Add a key here only
// after deciding it cannot be used to reach credentials or remote hosts.
var allowedKeys = map[string]bool{
	"watchdog_enabled":                  true,
	"watchdog_idle_enabled":             true,
	"watchdog_busy_enabled":             true,
	"watchdog_idle_timeout":             true,
	"watchdog_busy_timeout":             true,
	"max_active_backends":               true,
	"force_eviction_when_busy":          true,
	"lru_eviction_max_retries":          true,
	"lru_eviction_retry_interval":       true,
	"vram_budget":                       true,
	"vram_persistent_cache":             true,
	"threads":                           true,
	"context_size":                      true,
	"f16":                               true,
	"artifact_download_concurrency":     true,
	"max_concurrent_backend_operations": true,
	"debug":                             true,
}

// Setup registers the Settings controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Settings"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(settingsv1beta1.SchemeGroupVersion.WithKind("Settings")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).Named(controllerName).WithOptions(o.ForControllerRuntime()).For(&settingsv1beta1.Settings{}).Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*settingsv1beta1.Settings)
	if !ok {
		return nil, errors.New(errNotSettings)
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
	hc, err := clients.NewLocalAIHTTPClient(ctx, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI HTTP client")
	}
	return &external{cfg: cfg, httpClient: hc}, nil
}

// parseSettings decodes spec.forProvider.settings and checks every key against
// the allow-list. The value must be a JSON object.
func parseSettings(raw string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.Wrap(err, "settings must be a JSON object")
	}
	if out == nil {
		return nil, errors.New("settings must be a JSON object")
	}
	var rejected []string
	for k := range out {
		if !allowedKeys[k] {
			rejected = append(rejected, k)
		}
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		return nil, errors.Errorf("settings keys are not allowed: %s", strings.Join(rejected, ", "))
	}
	return out, nil
}

// settingsHash digests the canonical settings. encoding/json sorts map keys, so
// equal settings hash equally. The settings are not secret: the allow-list
// excludes every credential-bearing key.
func settingsHash(s map[string]any) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", errors.Wrap(err, "cannot encode settings")
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type external struct {
	cfg        laiclient.Config
	httpClient *http.Client
}

func (e *external) Disconnect(_ context.Context) error { return nil }

// Observe reports the resource as existing once settings have been applied.
// Until then it reports it missing, so Create posts the settings. After that,
// a change to spec.forProvider.settings makes it out of date.
func (e *external) Observe(_ context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*settingsv1beta1.Settings)
	desired, err := parseSettings(cr.Spec.ForProvider.Settings)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	hash, err := settingsHash(desired)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	if cr.Status.AtProvider.AppliedHash == "" {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: cr.Status.AtProvider.AppliedHash == hash,
	}, nil
}

// post sends the settings to LocalAI. LocalAI merges them into its saved
// settings; keys not in the body are left unchanged.
func (e *external) post(ctx context.Context, settings map[string]any) error {
	body, err := json.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "cannot encode settings")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.cfg.BaseURL, "/")+settingsPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return errors.Wrap(readErr, "cannot read LocalAI response")
	}
	if closeErr != nil {
		return errors.Wrap(closeErr, "cannot close LocalAI response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.Errorf("LocalAI returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (e *external) apply(ctx context.Context, cr *settingsv1beta1.Settings) error {
	desired, err := parseSettings(cr.Spec.ForProvider.Settings)
	if err != nil {
		return err
	}
	hash, err := settingsHash(desired)
	if err != nil {
		return err
	}
	if err := e.post(ctx, desired); err != nil {
		return errors.Wrap(err, errPostSettings)
	}
	cr.Status.AtProvider.AppliedHash = hash
	return nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*settingsv1beta1.Settings)
	if err := e.apply(ctx, cr); err != nil {
		return managed.ExternalCreation{}, err
	}
	return managed.ExternalCreation{}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*settingsv1beta1.Settings)
	if err := e.apply(ctx, cr); err != nil {
		return managed.ExternalUpdate{}, err
	}
	return managed.ExternalUpdate{}, nil
}

// Delete leaves LocalAI's settings as they are. Removing a setting here would
// only restore LocalAI's default for a key, and the provider does not know
// what value LocalAI held before.
func (e *external) Delete(_ context.Context, _ resource.Managed) (managed.ExternalDelete, error) {
	return managed.ExternalDelete{}, nil
}
