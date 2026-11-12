package agent

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

	agentv1beta1 "github.com/rossigee/provider-localai/apis/agent/v1beta1"
)

func testKube(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1 scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func agentWithKeyRef(ns string, ref *xpv1.SecretKeySelector) *agentv1beta1.Agent {
	return &agentv1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "bookkeeping"},
		Spec: agentv1beta1.AgentSpec{
			ForProvider: agentv1beta1.AgentParameters{APIKeySecretRef: ref},
		},
	}
}

func TestResolveAgentAPIKeyReadsSecretInAgentNamespace(t *testing.T) {
	kube := testKube(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "agent-key"},
		Data:       map[string][]byte{"token": []byte("  agent-secret \n")},
	})
	cr := agentWithKeyRef("tenant-a", &xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{Namespace: "tenant-a", Name: "agent-key"},
		Key:             "token",
	})
	got, err := resolveAgentAPIKey(context.Background(), kube, cr)
	if err != nil {
		t.Fatalf("resolveAgentAPIKey: %v", err)
	}
	if got != "agent-secret" {
		t.Errorf("key = %q, want trimmed agent-secret", got)
	}
}

func TestResolveAgentAPIKeyRejectsCrossNamespaceSecret(t *testing.T) {
	// The Agent must not be able to copy a Secret from another namespace.
	kube := testKube(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "crossplane-system", Name: "admin-key"},
		Data:       map[string][]byte{"token": []byte("admin")},
	})
	cr := agentWithKeyRef("tenant-a", &xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{Namespace: "crossplane-system", Name: "admin-key"},
		Key:             "token",
	})
	_, err := resolveAgentAPIKey(context.Background(), kube, cr)
	if err == nil || !strings.Contains(err.Error(), "Agent's namespace") {
		t.Fatalf("expected cross-namespace rejection, got %v", err)
	}
}

func TestResolveAgentAPIKeyNoRef(t *testing.T) {
	got, err := resolveAgentAPIKey(context.Background(), testKube(t), agentWithKeyRef("tenant-a", nil))
	if err != nil || got != "" {
		t.Fatalf("got (%q, %v), want empty key and no error", got, err)
	}
}
