package agenttask

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	taskv1beta1 "github.com/rossigee/provider-localai/apis/agenttask/v1beta1"
)

func taskCR() *taskv1beta1.AgentTask {
	return &taskv1beta1.AgentTask{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "digest"},
		Spec: taskv1beta1.AgentTaskSpec{ForProvider: taskv1beta1.AgentTaskParameters{
			Name:   "digest",
			Model:  "m1",
			Prompt: "Summarise the inbox",
		}},
	}
}

func taskExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

const matchingTask = `{"id":"t1","name":"digest","description":"","model":"m1","prompt":"Summarise the inbox","enabled":true,"created_at":"2026-11-15T00:00:00Z","updated_at":"2026-11-15T00:00:00Z"}`

func TestObserveWithoutExternalNameDoesNotExist(t *testing.T) {
	var requests int
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	obs, err := e.Observe(context.Background(), taskCR())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists || requests != 0 {
		t.Errorf("observe = %+v with %d request(s), want missing and no request", obs, requests)
	}
}

func TestObserveMatchingTaskIsUpToDate(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/tasks/t1" {
			t.Errorf("path = %s, want /api/agent/tasks/t1", r.URL.Path)
		}
		_, _ = io.WriteString(w, matchingTask)
	})
	cr := taskCR()
	meta.SetExternalName(cr, "t1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want existing and up to date", obs)
	}
}

func TestObserveDetectsDisabledDrift(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"t1","name":"digest","description":"","model":"m1","prompt":"Summarise the inbox","enabled":false}`)
	})
	cr := taskCR()
	meta.SetExternalName(cr, "t1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want drift when LocalAI has the task disabled", obs)
	}
}

func TestObserveMissingTaskDoesNotExist(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := taskCR()
	meta.SetExternalName(cr, "t1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Error("ResourceExists = true for a 404")
	}
}

func TestCreateRecordsIDAsExternalName(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/tasks" {
			t.Errorf("request = %s %s, want POST /api/agent/tasks", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"t9"}`)
	})
	cr := taskCR()
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := meta.GetExternalName(cr); got != "t9" {
		t.Errorf("external name = %q, want t9", got)
	}
}

func TestUpdatePutsToIDPath(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/agent/tasks/t1" {
			t.Errorf("request = %s %s, want PUT /api/agent/tasks/t1", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"message":"Task updated"}`)
	})
	cr := taskCR()
	meta.SetExternalName(cr, "t1")
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestDeleteToleratesMissingTask(t *testing.T) {
	e := taskExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/agent/tasks/t1" {
			t.Errorf("request = %s %s, want DELETE /api/agent/tasks/t1", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := taskCR()
	meta.SetExternalName(cr, "t1")
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete of a missing task returned %v, want nil", err)
	}
}
