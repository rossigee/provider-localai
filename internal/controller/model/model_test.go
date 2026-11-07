package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
)

func testExternal(t *testing.T, h http.HandlerFunc) (*external, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}, srv
}

func newModelCR(name string) *modelv1beta1.Model {
	return &modelv1beta1.Model{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "crossplane-system"},
		Spec: modelv1beta1.ModelSpec{
			ForProvider: modelv1beta1.ModelParameters{
				Name:    name,
				Backend: "localai-proxy",
			},
			ManagedResourceSpec: xpv1.ManagedResourceSpec{
				ProviderConfigReference: &xpv1.ProviderConfigReference{Name: "localai-provider-config"},
			},
		},
	}
}

const goodConfig = `
name: zen-qwen3.8-flash
backend: localai-proxy
parameters:
  model: qwen3.8-flash
known_usecases:
  - chat
`

func TestDesiredModelConfigParsesYAML(t *testing.T) {
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	cfg, err := desiredModelConfig(cr)
	if err != nil {
		t.Fatalf("desiredModelConfig: %v", err)
	}
	if cfg["name"] != "zen-qwen3.8-flash" || cfg["backend"] != "localai-proxy" {
		t.Errorf("cfg = %+v", cfg)
	}
	params, ok := cfg["parameters"].(map[string]any)
	if !ok || params["model"] != "qwen3.8-flash" {
		t.Errorf("parameters = %v", cfg["parameters"])
	}
}

func TestDesiredModelConfigRejectsNameMismatch(t *testing.T) {
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = "name: some-other-model\nbackend: localai-proxy\n"
	if _, err := desiredModelConfig(cr); err == nil {
		t.Fatal("expected an error for a config whose name differs from the CR name")
	}
}

func TestDesiredModelConfigRejectsBadYAML(t *testing.T) {
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = "name: [unterminated\n"
	if _, err := desiredModelConfig(cr); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestCreateImportsModel(t *testing.T) {
	var method, path string
	var body map[string]any
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = io.WriteString(w, `{"success":true,"message":"Model configuration created successfully","filename":"zen-qwen3.8-flash.yaml"}`)
	})

	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if method != http.MethodPost || path != "/models/import" {
		t.Errorf("request = %s %s, want POST /models/import", method, path)
	}
	if body["name"] != "zen-qwen3.8-flash" || body["backend"] != "localai-proxy" {
		t.Errorf("body = %+v", body)
	}
	if cr.Status.AtProvider.ConfigHash == "" {
		t.Error("Create did not record the config hash in status")
	}
}

func TestCreateSurfacesAPIError(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	})
	cr := newModelCR("zen-qwen3.8-flash")
	if _, err := e.Create(context.Background(), cr); err == nil {
		t.Fatal("expected a wrapped API error")
	}
}

func TestCreateReportsRejectedConfig(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"message":"invalid configuration"}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	if _, err := e.Create(context.Background(), cr); err == nil || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("expected rejection error mentioning the message, got %v", err)
	}
}

func TestCreateAcceptsQueuedJobEnvelope(t *testing.T) {
	// A config referencing remote assets is queued like a gallery install:
	// answered with a job envelope instead of success:true.
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"uuid":"job-9","status":"https://localai.example.com/models/jobs/job-9"}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestObserveReportsMissingModel(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"qwen3-8b","object":"model"}]}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Error("ResourceExists = true for a model LocalAI does not list, want false")
	}
}

func TestObserveAdoptsExistingModelAsBaseline(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"zen-qwen3.8-flash","object":"model"}]}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want exists and up to date (adopted)", obs)
	}
	if cr.Status.AtProvider.ConfigHash == "" {
		t.Error("adoption did not record the desired hash in status")
	}
}

func TestObserveDetectsDrift(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"zen-qwen3.8-flash","object":"model"}]}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	// Simulate a previous reconcile that applied a different config.
	cr.Status.AtProvider.ConfigHash = "stale-hash"
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want exists but not up to date after a spec change", obs)
	}
}

func TestDeleteCallsDeletePath(t *testing.T) {
	var method, path string
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = io.WriteString(w, `{"uuid":"job-3","status":"https://localai.example.com/models/jobs/job-3","deletion":true,"processed":true,"message":"completed"}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if method != http.MethodPost || path != "/models/delete/zen-qwen3.8-flash" {
		t.Errorf("request = %s %s, want POST /models/delete/zen-qwen3.8-flash", method, path)
	}
}

func TestUpdateReimportsModel(t *testing.T) {
	var path string
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["temperature"] != float64(0.5) {
			t.Errorf("temperature = %v, want 0.5", body["temperature"])
		}
		_, _ = io.WriteString(w, `{"success":true,"message":"Model configuration created successfully","filename":"zen-qwen3.8-flash.yaml"}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig + "temperature: 0.5\n"
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if path != "/models/import" {
		t.Errorf("path = %s, want /models/import", path)
	}
}
