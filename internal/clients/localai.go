package clients

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	laiclient "github.com/rossigee/localai-client/client"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

// GetLocalAIConfig resolves a ProviderConfig into a laiclient.Config.
func GetLocalAIConfig(ctx context.Context, pc *v1beta1.ProviderConfig, kube client.Client) (laiclient.Config, error) {
	cfg := laiclient.Config{
		BaseURL:  pc.Spec.BaseURL,
		TokenURL: pc.Spec.TokenURL,
		ClientID: pc.Spec.ClientID,
	}

	if pc.Spec.ClientSecretRef == nil {
		return cfg, errors.New("clientSecretRef is required")
	}

	secret := &corev1.Secret{}
	nn := types.NamespacedName{
		Name:      pc.Spec.ClientSecretRef.Name,
		Namespace: pc.Spec.ClientSecretRef.Namespace,
	}
	if err := kube.Get(ctx, nn, secret); err != nil {
		return cfg, errors.Wrap(err, "cannot get client secret")
	}

	key := pc.Spec.ClientSecretRef.Key
	if key == "" {
		key = "clientSecret"
	}
	raw, ok := secret.Data[key]
	if !ok {
		return cfg, errors.Errorf("key %q not found in secret %s/%s", key, nn.Namespace, nn.Name)
	}

	cfg.ClientSecret = string(raw)
	return cfg, nil
}
