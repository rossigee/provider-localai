package clients

import (
	"context"
	"strings"
	"testing"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

func testKube(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1 scheme: %v", err)
	}
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("v1beta1 scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func secret(ns, name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Data:       data,
	}
}

func providerConfig(ns, name string, creds *v1beta1.ProviderConfigSpec) *v1beta1.ProviderConfig {
	pc := &v1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	if creds != nil {
		pc.Spec = *creds
	}
	pc.Spec.BaseURL = "https://localai.example.com"
	return pc
}

func TestGetProviderConfigUsesOwnNamespace(t *testing.T) {
	kube := testKube(t,
		providerConfig("crossplane-system", "localai", nil),
		providerConfig("tenant-a", "localai", nil),
	)
	pc, err := GetProviderConfig(context.Background(), kube, "tenant-a", &xpv1.ProviderConfigReference{Name: "localai"})
	if err != nil {
		t.Fatalf("GetProviderConfig: %v", err)
	}
	if pc.GetNamespace() != "tenant-a" {
		t.Errorf("resolved ProviderConfig in %q, want tenant-a", pc.GetNamespace())
	}
}

func TestGetProviderConfigDoesNotReachAdminNamespace(t *testing.T) {
	kube := testKube(t, providerConfig("crossplane-system", "localai", nil))
	if _, err := GetProviderConfig(context.Background(), kube, "tenant-a", &xpv1.ProviderConfigReference{Name: "localai"}); err == nil {
		t.Fatal("a resource in tenant-a resolved the ProviderConfig in crossplane-system")
	}
}

func TestGetProviderConfigRequiresReference(t *testing.T) {
	kube := testKube(t)
	if _, err := GetProviderConfig(context.Background(), kube, "tenant-a", nil); err == nil {
		t.Fatal("expected an error for a missing providerConfigRef")
	}
}

func credsSpec(ref xpv1.SecretReference) *v1beta1.ProviderConfigSpec {
	return &v1beta1.ProviderConfigSpec{
		Credentials: &v1beta1.ProviderCredentials{
			Source: xpv1.CredentialsSourceSecret,
			CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
				SecretRef: &xpv1.SecretKeySelector{SecretReference: ref, Key: "token"},
			},
		},
	}
}

func TestGetLocalAIConfigReadsSecretInOwnNamespace(t *testing.T) {
	pc := providerConfig("tenant-a", "localai", credsSpec(xpv1.SecretReference{Namespace: "tenant-a", Name: "key"}))
	kube := testKube(t, secret("tenant-a", "key", map[string][]byte{"token": []byte("  sk-test \n")}))
	cfg, err := GetLocalAIConfig(context.Background(), pc, kube)
	if err != nil {
		t.Fatalf("GetLocalAIConfig: %v", err)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want trimmed sk-test", cfg.APIKey)
	}
}

func TestGetLocalAIConfigRejectsCrossNamespaceCredentials(t *testing.T) {
	// A tenant-owned ProviderConfig must not read the admin's Secret.
	pc := providerConfig("tenant-a", "localai", credsSpec(xpv1.SecretReference{Namespace: "crossplane-system", Name: "admin-key"}))
	kube := testKube(t, secret("crossplane-system", "admin-key", map[string][]byte{"token": []byte("admin")}))
	_, err := GetLocalAIConfig(context.Background(), pc, kube)
	if err == nil || !strings.Contains(err.Error(), "must reference a Secret in namespace") {
		t.Fatalf("expected cross-namespace rejection, got %v", err)
	}
}

func TestGetLocalAIConfigRejectsCrossNamespaceCA(t *testing.T) {
	pc := providerConfig("tenant-a", "localai", credsSpec(xpv1.SecretReference{Namespace: "tenant-a", Name: "key"}))
	pc.Spec.RootCACertificateRef = &xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{Namespace: "crossplane-system", Name: "ca"},
		Key:             "ca.crt",
	}
	kube := testKube(t,
		secret("tenant-a", "key", map[string][]byte{"token": []byte("k")}),
		secret("crossplane-system", "ca", map[string][]byte{"ca.crt": []byte("pem")}),
	)
	_, err := GetLocalAIConfig(context.Background(), pc, kube)
	if err == nil || !strings.Contains(err.Error(), "spec.rootCACertificateRef") {
		t.Fatalf("expected cross-namespace CA rejection, got %v", err)
	}
}
