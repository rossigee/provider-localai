package settings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

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
	errGetSettings      = "cannot read LocalAI settings"

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
//
// blockedKeys are keys LocalAI rejects on /api/settings because they are managed
// elsewhere (e.g. on the add-on owner). They are reported with a specific
// message rather than the generic "not allowed" one.
var blockedKeys = map[string]string{
	"auto_upgrade_addons": "auto_upgrade_addons is set on the add-on owner, not through /api/settings",
}

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
	"max_concurrent_model_operations":   true,
	"debug":                             true,
}

// typedKeys are scalar settings that only accept JSON literals of a specific
// shape. Everything in allowedKeys without a value validator keeps whatever
// JSON type the user wrote (LocalAI is tolerant of e.g. numeric strings there,
// and matching its behaviour exactly is not worth the churn); the keys below
// carry values whose type matters for correctness, so they are checked here.
var boolKeys = map[string]bool{
	"size_aware_eviction":             true,
	"memory_reclaimer_enabled":        true,
	"distributed_disk_headroom_check": true,
	"home_page_area_enabled":          true,
	"shared_pages_enabled":            true,
	"user_page_shares_enabled":        true,
}

var unitFloatKeys = map[string]bool{ // 0.0-1.0 fraction
	"memory_reclaimer_threshold": true,
}

var nonNegativeIntKeys = map[string]bool{
	"tracing_max_items":      true,
	"tracing_max_body_bytes": true,
}

var durationKeys = map[string]bool{
	"watchdog_interval":        true,
	"open_responses_store_ttl": true, // e.g. "1h", "30m", "0" = no expiration
}

// chatsValues are the chats_* keys a Settings resource may set, each with the
// values it accepts. They are kept out of allowedKeys because they gate tools,
// and every other chats_* key stays rejected. chats_tool_search only defers the
// large tool groups behind search tools, so it can be set to any mode.
// chats_localai_admin may only be turned off: the resource can close the LocalAI
// admin tools but can never open them.
var chatsValues = map[string][]any{
	"chats_tool_search":   {"auto", "on", "off"},
	"chats_localai_admin": {false},
}

// brandingKeys are the LocalAI instance branding settings: the name and
// tagline shown on the login page, and the uploaded logo/favicon files
// served at /branding/asset/<kind>. They carry no credentials and cannot
// reach remote hosts: the filename keys are validated to a safe basename
// (no path separators, traversal or control characters) so a Settings
// resource cannot point LocalAI at a file outside the branding directory.
const (
	brandingFilenameMaxLen = 128
	brandingTextMaxLen     = 256
)

var brandingKeys = map[string]string{
	"instance_name":        "text",
	"instance_tagline":     "text",
	"logo_file":            "filename",
	"logo_horizontal_file": "filename",
	"favicon_file":         "filename",
}

// validateBrandingValue checks a branding value. A "filename" must be a
// plain basename: no path separators, no "." or "..", no control characters,
// at most 128 characters. An empty string is accepted as a reset to the
// LocalAI default branding. A "text" value is free text, at most 256
// characters, with no control characters.
func validateBrandingValue(key, kind string, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("value must be a string")
	}
	if kind == "filename" {
		if len(s) > brandingFilenameMaxLen {
			return fmt.Errorf("filename must be at most %d characters", brandingFilenameMaxLen)
		}
		if s != "" {
			if strings.ContainsAny(s, "/\\") {
				return errors.New("filename must not contain path separators")
			}
			if s == "." || s == ".." {
				return errors.New("filename must not be . or ..")
			}
		}
	} else if len(s) > brandingTextMaxLen {
		return fmt.Errorf("value must be at most %d characters", brandingTextMaxLen)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return errors.New("value contains control characters")
		}
	}
	return nil
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
	for k, v := range out {
		if msg, ok := blockedKeys[k]; ok {
			return nil, errors.New(msg)
		}
		if allowedKeys[k] {
			continue
		}
		if kind, ok := brandingKeys[k]; ok {
			if err := validateBrandingValue(k, kind, v); err != nil {
				return nil, errors.Wrapf(err, "settings key %q", k)
			}
			continue
		}
		if boolKeys[k] {
			if _, ok := v.(bool); !ok {
				return nil, errors.Errorf("settings key %q must be a boolean", k)
			}
			continue
		}
		if unitFloatKeys[k] {
			f, ok := v.(float64)
			if !ok {
				return nil, errors.Errorf("settings key %q must be a number between 0 and 1", k)
			}
			if f < 0 || f > 1 {
				return nil, errors.Errorf("settings key %q must be between 0 and 1", k)
			}
			continue
		}
		if nonNegativeIntKeys[k] {
			f, ok := v.(float64)
			if !ok {
				return nil, errors.Errorf("settings key %q must be an integer", k)
			}
			if f < 0 || f != float64(int64(f)) {
				return nil, errors.Errorf("settings key %q must be a non-negative integer", k)
			}
			continue
		}
		if durationKeys[k] {
			s, ok := v.(string)
			if !ok {
				return nil, errors.Errorf("settings key %q must be a duration string (e.g. \"5m\", \"0\")", k)
			}
			if s == "" {
				return nil, errors.Errorf("settings key %q must not be empty", k)
			}
			if d, err := time.ParseDuration(s); err != nil || d < 0 {
				return nil, errors.Errorf("settings key %q must be a non-negative duration (e.g. \"5m\", \"0\")", k)
			}
			continue
		}
		if allowed, ok := chatsValues[k]; ok {
			if !containsValue(allowed, v) {
				return nil, errors.Errorf("settings value not allowed for %s: %v", k, v)
			}
			continue
		}
		rejected = append(rejected, k)
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		return nil, errors.Errorf("settings keys are not allowed: %s", strings.Join(rejected, ", "))
	}
	return out, nil
}

// containsValue reports whether v equals one of allowed. JSON decodes booleans
// and strings to bool and string, which reflect.DeepEqual compares exactly.
func containsValue(allowed []any, v any) bool {
	for _, a := range allowed {
		if reflect.DeepEqual(a, v) {
			return true
		}
	}
	return false
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
// Until then it reports it missing, so Create posts the settings. After that it
// reads the managed keys back from LocalAI: a change to spec.forProvider.settings,
// or a change made in LocalAI itself, makes the resource out of date. Keys the
// spec does not set are not read back in a way that matters, so they are not
// compared.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
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
		// Create records the hash in status, but Crossplane's update after Create
		// overwrites that status, so the hash can be lost and the settings would
		// be posted again on every reconcile. Observe keeps the status it writes,
		// so when LocalAI already holds the desired settings, adopt them here.
		current, err := e.current(ctx)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errGetSettings)
		}
		if !settingsMatch(desired, current) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		cr.Status.AtProvider.AppliedHash = hash
	}
	cr.Status.SetConditions(xpv1.Available())
	upToDate := cr.Status.AtProvider.AppliedHash == hash
	if upToDate {
		current, err := e.current(ctx)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errGetSettings)
		}
		upToDate = settingsMatch(desired, current)
	}
	return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: upToDate}, nil
}

// current reads LocalAI's runtime settings. The response carries credentials
// for an authorized administrator; only the keys the spec sets are kept.
func (e *external) current(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(e.cfg.BaseURL, "/")+settingsPath, nil)
	if err != nil {
		return nil, err
	}
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, errors.Wrap(readErr, "cannot read LocalAI response")
	}
	if closeErr != nil {
		return nil, errors.Wrap(closeErr, "cannot close LocalAI response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("LocalAI returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, "cannot decode LocalAI settings")
	}
	return out, nil
}

// settingsMatch reports whether every key the spec sets has the same value in
// LocalAI. Values are compared after a JSON round trip, so numbers match however
// they were decoded.
func settingsMatch(desired, current map[string]any) bool {
	for k, want := range desired {
		got, ok := current[k]
		if !ok {
			return false
		}
		if !jsonEqual(want, got) {
			return false
		}
	}
	return true
}

func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var av, bv any
	if json.Unmarshal(ab, &av) != nil || json.Unmarshal(bb, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
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
