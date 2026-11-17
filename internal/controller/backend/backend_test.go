package backend

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	backendv1beta1 "github.com/rossigee/provider-localai/apis/backend/v1beta1"
)

func backendCR() *backendv1beta1.Backend {
	return &backendv1beta1.Backend{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "llama"},
		Spec:       backendv1beta1.BackendSpec{ForProvider: backendv1beta1.BackendParameters{Name: "llama-cpp"}},
	}
}

func backendExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

func listing(installed bool) string {
	if installed {
		return `{"backends":[{"id":"llama-cpp","name":"llama-cpp","gallery":"localai","installed":true}],"totalPages":1}`
	}
	return `{"backends":[{"id":"llama-cpp","name":"llama-cpp","gallery":"localai","installed":false}],"totalPages":1}`
}

func TestObserveInstalledBackendExists(t *testing.T) {
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, listing(true))
	})
	obs, err := e.Observe(context.Background(), backendCR())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Error("ResourceExists = false for an installed backend")
	}
}

func TestObserveMissingBackendDoesNotExist(t *testing.T) {
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, listing(false))
	})
	obs, err := e.Observe(context.Background(), backendCR())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Error("ResourceExists = true for a backend that is not installed")
	}
}

func TestObserveWaitsForRunningInstall(t *testing.T) {
	var listed bool
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/backends/job/job-1":
			_, _ = io.WriteString(w, `{"processed":false,"progress":30,"queued":false}`)
		case "/api/backends":
			listed = true
			_, _ = io.WriteString(w, listing(false))
		}
	})
	cr := backendCR()
	cr.Status.AtProvider.PendingJobID = "job-1"
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want the running install treated as in progress", obs)
	}
	if listed {
		t.Error("the listing was read while the job was still running")
	}
}

func TestObserveReportsFailedInstallAndClearsJob(t *testing.T) {
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"processed":true,"progress":100,"error":"download failed"}`)
	})
	cr := backendCR()
	cr.Status.AtProvider.PendingJobID = "job-1"
	if _, err := e.Observe(context.Background(), cr); err == nil {
		t.Fatal("Observe hid a failed install")
	}
	if cr.Status.AtProvider.PendingJobID != "" {
		t.Error("the failed job was not cleared, so the next reconcile would not retry")
	}
}

func TestCreateStartsInstallAndRecordsJob(t *testing.T) {
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/backends/install/llama-cpp" {
			t.Errorf("request = %s %s, want POST /api/backends/install/llama-cpp", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"jobID":"job-1"}`)
	})
	cr := backendCR()
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cr.Status.AtProvider.PendingJobID != "job-1" {
		t.Errorf("pending job = %q, want job-1", cr.Status.AtProvider.PendingJobID)
	}
}

func TestDeleteStartsUninstallOnce(t *testing.T) {
	var deletes int
	e := backendExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/backends/delete/llama-cpp" {
			deletes++
			_, _ = io.WriteString(w, `{"jobID":"job-2"}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	cr := backendCR()
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cr.Status.AtProvider.PendingJobID != "job-2" {
		t.Errorf("pending job = %q, want job-2", cr.Status.AtProvider.PendingJobID)
	}
	// A second Delete while the job is pending must not request another uninstall.
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if deletes != 1 {
		t.Errorf("uninstall requested %d times, want 1", deletes)
	}
}
