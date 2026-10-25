package clients

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	laiclient "github.com/rossigee/localai-client/client"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

// GetLocalAIConfig resolves a ProviderConfig into a laiclient.Config.
//
// LocalAI's API only accepts API keys (the legacy LOCALAI_API_KEY, or a
// per-user key) and browser session IDs. An OIDC access token from the client
// credentials grant is rejected with "An authentication key is required", so
// the supported credential is a LocalAI API key held in a Secret, matching the
// credential shape used by the other providers. The OAuth2 fields are kept as
// a deprecated fallback for deployments fronted by something that does accept
// OAuth2 bearer tokens.
func GetLocalAIConfig(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) (laiclient.Config, error) {
	cfg := laiclient.Config{
		BaseURL: pc.Spec.BaseURL,
	}

	switch {
	case pc.Spec.Credentials != nil:
		apiKey, err := resolveAPIKey(ctx, pc, kube)
		if err != nil {
			return cfg, err
		}
		cfg.APIKey = apiKey

	case pc.Spec.ClientSecretRef != nil:
		// Deprecated OIDC client credentials.
		cfg.TokenURL = pc.Spec.TokenURL
		cfg.ClientID = pc.Spec.ClientID

		secret, err := getSecret(ctx, kube, pc.Spec.ClientSecretRef.Namespace, pc.Spec.ClientSecretRef.Name)
		if err != nil {
			return cfg, err
		}
		key := pc.Spec.ClientSecretRef.Key
		if key == "" {
			key = "clientSecret"
		}
		raw, ok := secret.Data[key]
		if !ok {
			return cfg, errors.Errorf("key %q not found in secret %s/%s", key, pc.Spec.ClientSecretRef.Namespace, pc.Spec.ClientSecretRef.Name)
		}
		cfg.ClientSecret = string(raw)

	default:
		return cfg, errors.New("no credentials configured: set spec.credentials.secretRef with a LocalAI API key")
	}

	if pc.Spec.RootCACertificateRef != nil {
		caSecret, err := getSecret(ctx, kube, pc.Spec.RootCACertificateRef.Namespace, pc.Spec.RootCACertificateRef.Name)
		if err != nil {
			return cfg, errors.Wrap(err, "cannot get CA certificate secret")
		}
		caKey := pc.Spec.RootCACertificateRef.Key
		if caKey == "" {
			caKey = "ca.crt"
		}
		caRaw, ok := caSecret.Data[caKey]
		if !ok {
			return cfg, errors.Errorf("key %q not found in CA secret %s/%s", caKey, pc.Spec.RootCACertificateRef.Namespace, pc.Spec.RootCACertificateRef.Name)
		}
		cfg.RootCACertificate = string(caRaw)
	}

	return cfg, nil
}

// resolveAPIKey reads the LocalAI API key from the configured credential secret.
func resolveAPIKey(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) (string, error) {
	creds := pc.Spec.Credentials
	if creds.Source != xpv1.CredentialsSourceSecret {
		return "", errors.Errorf("unsupported credentials source %q: only Secret is supported", creds.Source)
	}
	if creds.SecretRef == nil {
		return "", errors.New("credentials.secretRef is required when credentials.source is Secret")
	}

	ref := creds.SecretRef
	key := ref.Key
	if key == "" {
		key = "token"
	}

	secret, err := getSecret(ctx, kube, ref.Namespace, ref.Name)
	if err != nil {
		return "", err
	}
	raw, ok := secret.Data[key]
	if !ok {
		return "", errors.Errorf("key %q not found in secret %s/%s", key, ref.Namespace, ref.Name)
	}

	apiKey := strings.TrimSpace(string(raw))
	if apiKey == "" {
		return "", errors.Errorf("key %q in secret %s/%s is empty", key, ref.Namespace, ref.Name)
	}
	return apiKey, nil
}

func getSecret(ctx context.Context, kube client.Client, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	if err := kube.Get(ctx, nn, secret); err != nil {
		return nil, errors.Wrapf(err, "cannot get secret %s/%s", namespace, name)
	}
	return secret, nil
}
