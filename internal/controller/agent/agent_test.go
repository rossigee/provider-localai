package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	laiclient "github.com/rossigee/localai-client/client"
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

// resolveAgentAPIKey returns only the API key from resolveCredentials.
func resolveAgentAPIKey(ctx context.Context, kube client.Client, cr *agentv1beta1.Agent) (string, error) {
	creds, err := resolveCredentials(ctx, kube, cr)
	return creds.apiKey, err
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

func TestUpdateFailsWhenCurrentAgentUnreadable(t *testing.T) {
	// If the current agent cannot be read, its KB and memory settings are
	// unknown. The update must not be sent, or those settings would reset.
	var writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cr := agentWithKeyRef("tenant-a", nil)
	cr.Spec.ForProvider.Name = "bookkeeping"
	e := &external{client: c}
	if _, err := e.Update(context.Background(), cr); err == nil {
		t.Fatal("Update succeeded although the current agent could not be read")
	}
	if writes != 0 {
		t.Errorf("Update sent %d write request(s) without the current settings", writes)
	}
}

func TestResolveAgentAPIKeyNoRef(t *testing.T) {
	got, err := resolveAgentAPIKey(context.Background(), testKube(t), agentWithKeyRef("tenant-a", nil))
	if err != nil || got != "" {
		t.Fatalf("got (%q, %v), want empty key and no error", got, err)
	}
}

func TestObserveRecordsCredentialBaselineThenDetectsRotation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"bookkeeping","model":"qwen3-8b"}`))
	}))
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cr := agentWithKeyRef("tenant-a", nil)
	cr.Spec.ForProvider.Name = "bookkeeping"
	cr.Spec.ForProvider.Model = "qwen3-8b"

	e := &external{client: c, creds: credentials{apiKey: "key-1", versions: "tenant-a/agent-key@1"}}
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceUpToDate || cr.Status.AtProvider.SecretVersions == "" {
		t.Fatalf("first observation should record a baseline and be up to date, got %+v", obs)
	}

	// The Secret is rotated: the digest changes and the agent must be updated.
	e.creds = credentials{apiKey: "key-2", versions: "tenant-a/agent-key@2"}
	obs, err = e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe after rotation: %v", err)
	}
	if obs.ResourceUpToDate {
		t.Error("a rotated API key did not mark the agent out of date")
	}
}

func TestResolveCredentialsRecordsSecretVersionsNotValues(t *testing.T) {
	kube := testKube(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "agent-key", ResourceVersion: "42"},
		Data:       map[string][]byte{"token": []byte("super-secret-value")},
	})
	cr := agentWithKeyRef("tenant-a", &xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{Namespace: "tenant-a", Name: "agent-key"},
		Key:             "token",
	})
	creds, err := resolveCredentials(context.Background(), kube, cr)
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.versions != "tenant-a/agent-key@42" {
		t.Errorf("versions = %q, want tenant-a/agent-key@42", creds.versions)
	}
	if strings.Contains(creds.versions, "super-secret-value") {
		t.Error("versions contains the secret value")
	}
}

func TestResolveCredentialsRejectsInlineMCPToken(t *testing.T) {
	cr := agentWithKeyRef("tenant-a", nil)
	cr.Spec.ForProvider.MCPServers = []agentv1beta1.MCPServerSpec{{URL: "http://mcp", Token: "plaintext"}}
	_, err := resolveCredentials(context.Background(), testKube(t), cr)
	if err == nil || !strings.Contains(err.Error(), "tokenSecretRef") {
		t.Fatalf("expected an error pointing at tokenSecretRef, got %v", err)
	}
}

func TestResolveCredentialsReadsMCPTokenFromOwnNamespace(t *testing.T) {
	kube := testKube(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "mcp"},
		Data:       map[string][]byte{"token": []byte("mcp-secret")},
	})
	cr := agentWithKeyRef("tenant-a", nil)
	cr.Spec.ForProvider.MCPServers = []agentv1beta1.MCPServerSpec{{
		URL:            "http://mcp",
		TokenSecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Namespace: "tenant-a", Name: "mcp"}, Key: "token"},
	}}
	creds, err := resolveCredentials(context.Background(), kube, cr)
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if len(creds.mcpTokens) != 1 || creds.mcpTokens[0] != "mcp-secret" {
		t.Errorf("mcpTokens = %v, want [mcp-secret]", creds.mcpTokens)
	}
}

func TestResolveCredentialsRejectsCrossNamespaceMCPToken(t *testing.T) {
	cr := agentWithKeyRef("tenant-a", nil)
	cr.Spec.ForProvider.MCPServers = []agentv1beta1.MCPServerSpec{{
		URL:            "http://mcp",
		TokenSecretRef: &xpv1.SecretKeySelector{SecretReference: xpv1.SecretReference{Namespace: "crossplane-system", Name: "mcp"}, Key: "token"},
	}}
	_, err := resolveCredentials(context.Background(), testKube(t), cr)
	if err == nil || !strings.Contains(err.Error(), "Agent's namespace") {
		t.Fatalf("expected cross-namespace rejection, got %v", err)
	}
}
