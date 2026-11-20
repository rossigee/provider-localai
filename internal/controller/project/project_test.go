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

func TestDeleteProjectNotFound(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	cr.Status.AtProvider.ID = "myproj"
	_, err := e.Delete(context.Background(), cr)
	if err != nil {
		t.Fatalf("Delete (not found) should succeed, got: %v", err)
	}
}

func TestUpdateProject(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active"}`)
			return
		}
	})
	cr := projectCR("My Project")
	cr.Status.AtProvider.ID = "myproj"
	_, err := e.Update(context.Background(), cr)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestUpdateProjectArchive(t *testing.T) {
	var gotArchive bool
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active"}`)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/projects/myproj/archive" {
			gotArchive = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})
	cr := projectCR("My Project")
	cr.Status.AtProvider.ID = "myproj"
	archived := true
	cr.Spec.ForProvider.Archived = &archived
	_, err := e.Update(context.Background(), cr)
	if err != nil {
		t.Fatalf("Update (archive): %v", err)
	}
	if !gotArchive {
		t.Error("expected archive call")
	}
}

func TestUpdateProjectRestore(t *testing.T) {
	var gotRestore bool
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active"}`)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/projects/myproj/restore" {
			gotRestore = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})
	cr := projectCR("My Project")
	cr.Status.AtProvider.ID = "myproj"
	archived := false
	cr.Spec.ForProvider.Archived = &archived
	_, err := e.Update(context.Background(), cr)
	if err != nil {
		t.Fatalf("Update (restore): %v", err)
	}
	if !gotRestore {
		t.Error("expected restore call")
	}
}

func TestObserveProjectError(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := e.Observe(context.Background(), projectCR("myproj"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateProjectError(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	cr := projectCR("My Project")
	_, err := e.Create(context.Background(), cr)
	if err == nil {
		t.Fatal("expected error")
	}
}
