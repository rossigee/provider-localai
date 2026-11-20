package trigger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	triggerv1beta1 "github.com/rossigee/provider-localai/apis/trigger/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
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
	cfg := laiclient.Config{BaseURL: srv.URL, APIKey: "k"}
	c, err := laiclient.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hc, err := clients.NewLocalAIHTTPClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewLocalAIHTTPClient: %v", err)
	}
	return &external{client: c, cfg: cfg, httpClient: hc}
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

func TestObserveActs(t *testing.T) {
	cases := []struct {
		name     string
		want     []string
		held     string
		upToDate bool
	}{
		{"same acts in another order", []string{"act.spend", "act.files"}, `["act.files","act.spend"]`, true},
		{"missing act", []string{"act.spend", "act.files"}, `["act.files"]`, false},
		{"extra act", []string{"act.spend"}, `["act.spend","act.files"]`, false},
		{"explicit empty list", []string{}, `[]`, true},
		{"unset acts are left alone", nil, `["act.spend"]`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"id":"tr1","name":"digest","prompt":"Summarise the inbox","kind":"schedule","schedule":"daily 08:00","enabled":true,"acts":`+tc.held+`}`)
			})
			cr := triggerCR()
			cr.Spec.ForProvider.Acts = tc.want
			meta.SetExternalName(cr, "tr1")
			obs, err := e.Observe(context.Background(), cr)
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if !obs.ResourceExists || obs.ResourceUpToDate != tc.upToDate {
				t.Errorf("observe = %+v, want upToDate %v", obs, tc.upToDate)
			}
		})
	}
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
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

func TestCreateSendsActs(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		acts, ok := body["acts"].([]any)
		if !ok || len(acts) != 1 || acts[0] != "act.spend" {
			t.Errorf("acts = %v, want [act.spend]", body["acts"])
		}
		if body["agent_id"] != "ag1" {
			t.Errorf("agent_id = %v, want ag1 (embedded client fields must survive)", body["agent_id"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"tr9","enabled":true}`)
	})
	cr := triggerCR()
	cr.Spec.ForProvider.AgentID = "ag1"
	cr.Spec.ForProvider.Acts = []string{"act.spend"}
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateOmitsUnsetActs(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if _, ok := body["acts"]; ok {
			t.Errorf("unset acts were sent as %v", body["acts"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"tr9","enabled":true}`)
	})
	if _, err := e.Create(context.Background(), triggerCR()); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestUpdateSendsActs(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/chats/triggers/tr1" {
			t.Errorf("request = %s %s, want PUT /api/chats/triggers/tr1", r.Method, r.URL.Path)
		}
		body := readBody(t, r)
		acts, ok := body["acts"].([]any)
		if !ok || len(acts) != 1 || acts[0] != "act.spend" {
			t.Errorf("acts = %v, want [act.spend]", body["acts"])
		}
		_, _ = io.WriteString(w, `{"id":"tr1","enabled":true}`)
	})
	cr := triggerCR()
	cr.Spec.ForProvider.Acts = []string{"act.spend"}
	meta.SetExternalName(cr, "tr1")
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestUpdateSendsExplicitEmptyActs(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		acts, ok := body["acts"].([]any)
		if !ok || len(acts) != 0 {
			t.Errorf("acts = %v, want an explicit empty list", body["acts"])
		}
		_, _ = io.WriteString(w, `{"id":"tr1","enabled":true}`)
	})
	cr := triggerCR()
	cr.Spec.ForProvider.Acts = []string{}
	meta.SetExternalName(cr, "tr1")
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
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

func TestObserveSourceChat(t *testing.T) {
	cases := []struct {
		name     string
		set      string
		held     string
		upToDate bool
	}{
		{"matching source chat", "e85b653005ed9ac6fcab795f", "e85b653005ed9ac6fcab795f", true},
		{"drifted source chat", "e85b653005ed9ac6fcab795f", "aaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"unset is unmanaged", "", "e85b653005ed9ac6fcab795f", true},
		{"missing on the server drifts", "e85b653005ed9ac6fcab795f", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := ""
			if tc.held != "" {
				src = `,"source_chat_id":"` + tc.held + `"`
			}
			e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"id":"tr1","name":"digest","prompt":"Summarise the inbox","kind":"schedule","schedule":"daily 08:00","enabled":true`+src+`}`)
			})
			cr := triggerCR()
			cr.Spec.ForProvider.SourceChatID = tc.set
			meta.SetExternalName(cr, "tr1")
			obs, err := e.Observe(context.Background(), cr)
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if !obs.ResourceExists || obs.ResourceUpToDate != tc.upToDate {
				t.Errorf("observe = %+v, want upToDate %v", obs, tc.upToDate)
			}
		})
	}
}

func TestCreateSendsSourceChatID(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if body["source_chat_id"] != "e85b653005ed9ac6fcab795f" {
			t.Errorf("source_chat_id = %v, want e85b653005ed9ac6fcab795f", body["source_chat_id"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"tr9","enabled":true}`)
	})
	cr := triggerCR()
	cr.Spec.ForProvider.SourceChatID = "e85b653005ed9ac6fcab795f"
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateOmitsUnsetSourceChatID(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		if _, ok := body["source_chat_id"]; ok {
			t.Errorf("unset source chat id was sent as %v", body["source_chat_id"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"tr9","enabled":true}`)
	})
	if _, err := e.Create(context.Background(), triggerCR()); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestUpdateSendsSourceChatID(t *testing.T) {
	e := triggerExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/chats/triggers/tr1" {
			t.Errorf("request = %s %s, want PUT /api/chats/triggers/tr1", r.Method, r.URL.Path)
		}
		body := readBody(t, r)
		if body["source_chat_id"] != "e85b653005ed9ac6fcab795f" {
			t.Errorf("source_chat_id = %v, want e85b653005ed9ac6fcab795f", body["source_chat_id"])
		}
		_, _ = io.WriteString(w, `{"id":"tr1","enabled":true}`)
	})
	cr := triggerCR()
	cr.Spec.ForProvider.SourceChatID = "e85b653005ed9ac6fcab795f"
	meta.SetExternalName(cr, "tr1")
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
