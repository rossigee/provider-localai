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
	"github.com/rossigee/provider-localai/internal/clients"
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
	cfg := laiclient.Config{BaseURL: srv.URL, APIKey: "k"}
	c, err := laiclient.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := clients.NewRawClient(cfg)
	if err != nil {
		t.Fatalf("NewRawClient: %v", err)
	}
	return &external{client: c, raw: raw}, srv
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

func TestCreateMovesAgentIntoProject(t *testing.T) {
	var moved bool
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/chats/agents" {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"a9","name":"reviewer"}`)
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api/chats/agents/a9/project" {
			moved = true
			if r.URL.Query().Get("user_id") != "u1" {
				t.Errorf("user_id = %q, want u1", r.URL.Query().Get("user_id"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	cr := agentCR("reviewer")
	cr.Spec.ForProvider.Owner = "u1"
	cr.Spec.ForProvider.ProjectID = "p1"
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !moved {
		t.Error("Create did not call the move endpoint")
	}
	if got := cr.Status.AtProvider.ProjectID; got != "p1" {
		t.Errorf("status project = %q, want p1", got)
	}
}

func TestCreateAdoptsExistingAgentOnNameConflict(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats/agents":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":409,"message":"you already have an agent with this name","type":"conflict"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/chats/agents":
			_, _ = io.WriteString(w, `{"agents":[{"id":"legacy1","name":"reviewer","mcp_servers":[],"addons":[],"collections":[],"skills":[],"network":"off"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := agentCR("reviewer")
	meta.SetExternalName(cr, "reviewer")
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create adopting an existing agent returned %v, want nil", err)
	}
	if got := meta.GetExternalName(cr); got != "legacy1" {
		t.Errorf("external name = %q, want the adopted ID legacy1", got)
	}
}

func TestCreateConflictWithoutMatchingAgentFails(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":409,"message":"conflict","type":"conflict"}}`)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"agents":[]}`)
		}
	})
	if _, err := e.Create(context.Background(), agentCR("reviewer")); err == nil {
		t.Fatal("Create succeeded although the conflicting agent could not be found")
	}
}

const ownedAgentJSON = `{"id":"a1","owner":"tenant-user","name":"reviewer","model":"m1",` +
	`"instructions":"review things","description":"","mcp_servers":[],"addons":[],` +
	`"collections":["docs"],"skills":[],"network":"off"}`

func ownedAgentCR() *agentv1beta1.Agent {
	cr := agentCR("reviewer")
	cr.Spec.ForProvider.Owner = "tenant-user"
	return cr
}

func TestObserveUsesOwnerScope(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user_id"); got != "tenant-user" {
			t.Errorf("user_id = %q, want tenant-user", got)
		}
		_, _ = io.WriteString(w, ownedAgentJSON)
	})
	cr := ownedAgentCR()
	meta.SetExternalName(cr, "a1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing and up to date", obs)
	}
	if cr.Status.AtProvider.Owner != "tenant-user" {
		t.Errorf("status owner = %q, want tenant-user", cr.Status.AtProvider.Owner)
	}
}

func TestObserveDetectsOwnerDrift(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"a1","name":"reviewer","model":"m1","instructions":"review things",`+
			`"description":"","mcp_servers":[],"addons":[],"collections":["docs"],"skills":[],"network":"off"}`)
	})
	cr := ownedAgentCR()
	meta.SetExternalName(cr, "a1")
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing but not up to date", obs)
	}
}

func TestAgentLifecycleSendsOwnerScope(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user_id"); got != "tenant-user" {
			t.Errorf("%s %s user_id = %q, want tenant-user", r.Method, r.URL.Path, got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats/agents":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"a9","owner":"tenant-user","name":"reviewer"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/chats/agents/a9":
			_, _ = io.WriteString(w, ownedAgentJSON)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/chats/agents/a9":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := ownedAgentCR()
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := cr.Status.AtProvider.Owner; got != "tenant-user" {
		t.Errorf("status owner after create = %q, want tenant-user", got)
	}
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestCreateAdoptsExistingAgentWithOwnerScope(t *testing.T) {
	e, _ := agentExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("user_id"); got != "tenant-user" {
			t.Errorf("%s %s user_id = %q, want tenant-user", r.Method, r.URL.Path, got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chats/agents":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":409,"message":"you already have an agent with this name","type":"conflict"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/chats/agents":
			_, _ = io.WriteString(w, `{"agents":[{"id":"legacy1","owner":"tenant-user","name":"reviewer"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := ownedAgentCR()
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create adopting an existing agent returned %v, want nil", err)
	}
	if got := meta.GetExternalName(cr); got != "legacy1" {
		t.Errorf("external name = %q, want the adopted ID legacy1", got)
	}
	if got := cr.Status.AtProvider.Owner; got != "tenant-user" {
		t.Errorf("status owner after adoption = %q, want tenant-user", got)
	}
}
