package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	agentv1beta1 "github.com/rossigee/provider-localai/apis/agent/v1beta1"
)

func agentCR(name string) *agentv1beta1.Agent {
	return &agentv1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: name},
		Spec: agentv1beta1.AgentSpec{ForProvider: agentv1beta1.AgentParameters{
			Name:         "reviewer",
			Model:        "m1",
			Instructions: "review things",
			Collections:  []string{"docs"},
		}},
	}
}

func agentExternal(t *testing.T, h http.HandlerFunc) (*external, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}, srv
}

// matchingAgentJSON is what LocalAI reports for agentCR once it has been saved.
const matchingAgentJSON = `{"id":"a1","name":"reviewer","model":"m1","instructions":"review things","description":"",` +
	`"mcp_servers":[],"addons":[],"collections":["docs"],"skills":[],"network":"off"}`

func TestObserveWithoutExternalNameDoesNotExist(t *testing.T) {
	var requests int
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.WriteString(w, `{}`)
	})
	obs, err := e.Observe(context.Background(), agentCR("reviewer"))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Errorf("ResourceExists = true, want false before the agent has an external name")
	}
	if requests != 0 {
		t.Errorf("Observe sent %d request(s) without an external name, want 0", requests)
	}
}

func TestObserveLooksUpByExternalNameAndIsUpToDate(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chats/agents/a1" {
			t.Errorf("path = %s, want /api/chats/agents/a1", r.URL.Path)
		}
		_, _ = io.WriteString(w, matchingAgentJSON)
	})
	cr := agentCR("reviewer")
	meta.SetExternalName(cr, "a1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing and up to date", obs)
	}
	if cr.Status.AtProvider.ID != "a1" {
		t.Errorf("status ID = %q, want a1", cr.Status.AtProvider.ID)
	}
}

func TestObserveDetectsCollectionDrift(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"a1","name":"reviewer","model":"m1","instructions":"review things","mcp_servers":[],"addons":[],"collections":[],"skills":[],"network":"off"}`)
	})
	cr := agentCR("reviewer")
	meta.SetExternalName(cr, "a1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing but not up to date", obs)
	}
}

func TestObserveMissingAgentDoesNotExist(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := agentCR("reviewer")
	meta.SetExternalName(cr, "gone")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Errorf("ResourceExists = true, want false for a 404")
	}
}

func TestCreateRecordsLocalAIIDAsExternalName(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chats/agents" {
			t.Errorf("request = %s %s, want POST /api/chats/agents", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"a9","name":"reviewer"}`)
	})
	cr := agentCR("reviewer")
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := meta.GetExternalName(cr); got != "a9" {
		t.Errorf("external name = %q, want a9", got)
	}
}

func TestDeleteUsesExternalNameAndToleratesMissingAgent(t *testing.T) {
	var deleted string
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		deleted = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	cr := agentCR("reviewer")
	meta.SetExternalName(cr, "a1")
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete of an already-gone agent returned %v, want nil", err)
	}
	if deleted != "/api/chats/agents/a1" {
		t.Errorf("deleted path = %q, want /api/chats/agents/a1", deleted)
	}
}

func TestDeleteWithoutExternalNameSendsNothing(t *testing.T) {
	var requests int
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
	})
	if _, err := e.Delete(context.Background(), agentCR("reviewer")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if requests != 0 {
		t.Errorf("Delete sent %d request(s) without an external name, want 0", requests)
	}
}
