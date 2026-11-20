package project

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	projectv1beta1 "github.com/rossigee/provider-localai/apis/project/v1beta1"
)

func projectCR(name string) *projectv1beta1.Project {
	return &projectv1beta1.Project{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: name},
		Spec: projectv1beta1.ProjectSpec{ForProvider: projectv1beta1.ProjectParameters{
			Name: name,
		}},
	}
}

func projectExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

func TestObserveMissingProjectDoesNotExist(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	obs, err := e.Observe(context.Background(), projectCR("myproj"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Errorf("ResourceExists = true, want false")
	}
}

func TestObserveExistingProjectExists(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active","chat_count":0}`)
	})
	obs, err := e.Observe(context.Background(), projectCR("My Project"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Errorf("ResourceExists = false, want true")
	}
}

func TestCreateProject(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active"}`)
			return
		}
	})
	cr := projectCR("My Project")
	_, err := e.Create(context.Background(), cr)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestDeleteProject(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})
	cr := projectCR("My Project")
	cr.Status.AtProvider.ID = "myproj"
	_, err := e.Delete(context.Background(), cr)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
