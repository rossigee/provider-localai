package project

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
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
		if r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"projects":[]}`)
			return
		}
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
		if r.URL.Path == "/api/chats/projects/myproj" {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active","chat_count":0}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	meta.SetExternalName(cr, "myproj")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Errorf("ResourceExists = false, want true")
	}
}

func TestObserveAdoptsByNameAfterSlugExternalName404(t *testing.T) {
	// Crossplane's NameAsExternalName initializer sets external-name to the
	// resource name (a slug) when the annotation is empty. LocalAI resolves
	// GET /api/chats/projects/<id> by UUID only and 404s for a slug. Observe
	// must fall back to a name lookup and adopt the real UUID, otherwise it
	// reports ResourceExists=false and Create makes a duplicate project.
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chats/projects/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"uuid-1","name":"My Project","status":"active"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	meta.SetExternalName(cr, "my-project") // slug, not a UUID
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Errorf("ResourceExists = false, want true")
	}
	if got := meta.GetExternalName(cr); got != "uuid-1" {
		t.Errorf("external-name = %q, want uuid-1", got)
	}
}

func TestObserveAdoptsByName(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"p1","name":"My Project","status":"active"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Errorf("ResourceExists = false, want true")
	}
	if meta.GetExternalName(cr) != "p1" {
		t.Errorf("external-name = %q, want p1", meta.GetExternalName(cr))
	}
}

func TestObserveProjectDetectsDefaultsDrift(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chats/projects/p1" {
			_, _ = io.WriteString(w, `{"id":"p1","name":"My Project","status":"active",`+
				`"defaults":{"skills":["old-skill"]}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	cr.Spec.ForProvider.Defaults = &projectv1beta1.ProjectDefaults{
		Skills: []string{"new-skill"},
	}
	meta.SetExternalName(cr, "p1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceUpToDate {
		t.Error("ResourceUpToDate = true, want false when defaults drifted")
	}
}

func TestObserveProjectDetectsAttachedDrift(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chats/projects/p1" {
			_, _ = io.WriteString(w, `{"id":"p1","name":"My Project","status":"active",`+
				`"attached":{"skills":["old-skill"]}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	cr.Spec.ForProvider.Attached = &projectv1beta1.ProjectAttached{
		Skills: []string{"new-skill"},
	}
	meta.SetExternalName(cr, "p1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceUpToDate {
		t.Error("ResourceUpToDate = true, want false when attached drifted")
	}
}

func TestObserveProjectIgnoresDefaultOrder(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chats/projects/p1" {
			_, _ = io.WriteString(w, `{"id":"p1","name":"My Project","status":"active",`+
				`"defaults":{"skills":["b","a"]}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	cr := projectCR("My Project")
	cr.Spec.ForProvider.Defaults = &projectv1beta1.ProjectDefaults{
		Skills: []string{"a", "b"},
	}
	meta.SetExternalName(cr, "p1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceUpToDate {
		t.Error("ResourceUpToDate = false, want true when only order differs")
	}
}

func TestCreateProject(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"id":"myproj","name":"My Project","status":"active"}`)
			return
		}
	})
	cr := projectCR("My Project")
	_, err := e.Create(context.Background(), cr)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.GetExternalName(cr) != "myproj" {
		t.Errorf("external-name = %q, want myproj", meta.GetExternalName(cr))
	}
}

func TestCreateProjectAdoptsExisting(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/projects" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"boom"}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"p1","name":"My Project","status":"active"}]}`)
			return
		}
	})
	cr := projectCR("My Project")
	_, err := e.Create(context.Background(), cr)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.GetExternalName(cr) != "p1" {
		t.Errorf("external-name = %q, want p1", meta.GetExternalName(cr))
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
	cr := projectCR("myproj")
	meta.SetExternalName(cr, "myproj")
	_, err := e.Observe(context.Background(), cr)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateProjectError(t *testing.T) {
	e := projectExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/projects" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/chats/projects" {
			_, _ = io.WriteString(w, `{"projects":[]}`)
			return
		}
	})
	cr := projectCR("My Project")
	_, err := e.Create(context.Background(), cr)
	if err == nil {
		t.Fatal("expected error")
	}
}
