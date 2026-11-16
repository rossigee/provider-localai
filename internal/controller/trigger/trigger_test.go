package trigger

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	triggerv1beta1 "github.com/rossigee/provider-localai/apis/trigger/v1beta1"
)

func triggerCR() *triggerv1beta1.Trigger {
	enabled := true
	return &triggerv1beta1.Trigger{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "digest"},
		Spec: triggerv1beta1.TriggerSpec{ForProvider: triggerv1beta1.TriggerParameters{
			Name:     "digest",
			Prompt:   "Summarise the inbox",
			Kind:     "schedule",
			Schedule: "daily 08:00",
			Enabled:  &enabled,
		}},
	}
}

func triggerExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

const matchingTrigger = `{"id":"tr1","name":"digest","prompt":"Summarise the inbox","kind":"schedule","schedule":"daily 08:00","enabled":true,"summary":"daily 08:00"}`

func TestObserveWithoutExternalNameDoesNotExist(t *testing.T) {
	var requests int
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	obs, err := e.Observe(context.Background(), triggerCR())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists || requests != 0 {
		t.Errorf("observe = %+v with %d request(s), want missing and no request", obs, requests)
	}
}

func TestObserveMatchingTriggerIsUpToDate(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chats/triggers/tr1" {
			t.Errorf("path = %s, want /api/chats/triggers/tr1", r.URL.Path)
		}
		_, _ = io.WriteString(w, matchingTrigger)
	})
	cr := triggerCR()
	meta.SetExternalName(cr, "tr1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want existing and up to date", obs)
	}
}

func TestObserveDetectsScheduleDrift(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","name":"digest","prompt":"Summarise the inbox","kind":"schedule","schedule":"daily 09:00","enabled":true}`)
	})
	cr := triggerCR()
	meta.SetExternalName(cr, "tr1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want drift on the schedule", obs)
	}
}

func TestObserveDetectsDisabledDrift(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","name":"digest","prompt":"Summarise the inbox","kind":"schedule","schedule":"daily 08:00","enabled":false}`)
	})
	cr := triggerCR()
	meta.SetExternalName(cr, "tr1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceUpToDate {
		t.Error("a disabled trigger matched a spec that enables it")
	}
}

func TestObserveMissingTriggerDoesNotExist(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := triggerCR()
	meta.SetExternalName(cr, "tr1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Error("ResourceExists = true for a 404")
	}
}

func TestCreateRecordsIDAsExternalName(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chats/triggers" {
			t.Errorf("request = %s %s, want POST /api/chats/triggers", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"tr9","name":"digest","kind":"schedule","enabled":true}`)
	})
	cr := triggerCR()
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := meta.GetExternalName(cr); got != "tr9" {
		t.Errorf("external name = %q, want tr9", got)
	}
}

func TestDeleteToleratesMissingTrigger(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/chats/triggers/tr1" {
			t.Errorf("request = %s %s, want DELETE /api/chats/triggers/tr1", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := triggerCR()
	meta.SetExternalName(cr, "tr1")
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete of a missing trigger returned %v, want nil", err)
	}
}
