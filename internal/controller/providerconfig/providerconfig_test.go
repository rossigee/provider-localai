package providerconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

// checkAgainst builds a ProviderConfig whose API key is "good-key" and a
// fake LocalAI that accepts only that key.
func checkAgainst(t *testing.T, presentedKey string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "crossplane-system", Name: "creds"},
			Data:       map[string][]byte{"token": []byte(presentedKey)},
		},
	).Build()

	pc := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "crossplane-system", Name: "localai"},
		Spec: v1beta1.ProviderConfigSpec{
			BaseURL: srv.URL,
			Credentials: &v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{Namespace: "crossplane-system", Name: "creds"},
						Key:             "token",
					},
				},
			},
		},
	}
	r := &reconciler{kube: client.Client(kube)}
	return r.check(context.Background(), pc)
}

func TestCheckSucceedsWithAcceptedKey(t *testing.T) {
	if err := checkAgainst(t, "good-key"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestCheckFailsWithRejectedKey(t *testing.T) {
	if err := checkAgainst(t, "stale-key"); err == nil {
		t.Fatal("check passed although LocalAI rejected the key")
	}
}
