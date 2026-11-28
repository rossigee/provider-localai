package skill

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	skillv1beta1 "github.com/rossigee/provider-localai/apis/skill/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

func skillCR(name string) *skillv1beta1.Skill {
	return &skillv1beta1.Skill{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: name},
		Spec: skillv1beta1.SkillSpec{ForProvider: skillv1beta1.SkillParameters{
			Layer:   "personal",
			Name:    name,
			Content: "c",
		}},
	}
}

func skillExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := laiclient.Config{BaseURL: srv.URL, APIKey: "k"}
	c, err := laiclient.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := clients.NewRawClient(cfg)
	if err != nil {
		t.Fatalf("NewRawClient: %v", err)
	}
	return &external{client: c, raw: raw}
}

func TestSkillPathIncludesProjectForProjectLayer(t *testing.T) {
	p := skillv1beta1.SkillParameters{Layer: "project", Name: "overview", ProjectID: "abc123"}
	want := "/api/chats/skills/project/overview?project=abc123"
	if got := skillPath(p); got != want {
		t.Fatalf("skillPath = %q, want %q", got, want)
	}
}

func TestObserveProjectSkillUsesProjectQuery(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chats/skills/project/overview" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("project"); got != "p1" {
			t.Errorf("project = %q, want p1", got)
		}
		_, _ = io.WriteString(w, `{"name":"overview","layer":"project","description":"d","body":"c"}`)
	})
	cr := skillCR("overview")
	cr.Spec.ForProvider.Layer = "project"
	cr.Spec.ForProvider.ProjectID = "p1"
	cr.Spec.ForProvider.Description = "d"
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Fatalf("obs = %+v, want exists and up to date", obs)
	}
}

func TestObserveLooksUpByLayerAndName(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chats/skills/personal/research" {
			t.Errorf("path = %s, want /api/chats/skills/personal/research", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"name":"research","layer":"personal","description":"","body":"c","editable":true}`)
	})
	obs, err := e.Observe(context.Background(), skillCR("research"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing and up to date", obs)
	}
}

func TestObserveDetectsBodyDrift(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"name":"research","layer":"personal","description":"","body":"old","editable":true}`)
	})
	obs, err := e.Observe(context.Background(), skillCR("research"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing but not up to date", obs)
	}
}

func TestObserveMissingSkillDoesNotExist(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	obs, err := e.Observe(context.Background(), skillCR("research"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Errorf("ResourceExists = true, want false for a 404")
	}
}

func TestUpdatePutsToLayerAndName(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/chats/skills/personal/research" {
			t.Errorf("request = %s %s, want PUT /api/chats/skills/personal/research", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"name":"research","layer":"personal","description":"","body":"c","editable":true}`)
	})
	if _, err := e.Update(context.Background(), skillCR("research")); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestDeleteToleratesMissingSkill(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/chats/skills/personal/research" {
			t.Errorf("request = %s %s, want DELETE /api/chats/skills/personal/research", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	if _, err := e.Delete(context.Background(), skillCR("research")); err != nil {
		t.Fatalf("Delete of an already-gone skill returned %v, want nil", err)
	}
}
