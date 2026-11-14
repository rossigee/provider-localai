package settings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	settingsv1beta1 "github.com/rossigee/provider-localai/apis/settings/v1beta1"
)

func settingsCR(raw string) *settingsv1beta1.Settings {
	return &settingsv1beta1.Settings{
		ObjectMeta: metav1.ObjectMeta{Namespace: "crossplane-system", Name: "localai"},
		Spec:       settingsv1beta1.SettingsSpec{ForProvider: settingsv1beta1.SettingsParameters{Settings: raw}},
	}
}

// fakeLocalAI returns an external whose requests are recorded by the handler.
func fakeLocalAI(t *testing.T, status int, record func(r *http.Request, body []byte)) *external {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if record != nil {
			record(r, body)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	t.Cleanup(srv.Close)
	return &external{
		cfg:        laiclient.Config{BaseURL: srv.URL + "/", APIKey: "test-key"},
		httpClient: srv.Client(),
	}
}

func TestParseSettingsAcceptsAllowedKeys(t *testing.T) {
	s, err := parseSettings(`{"watchdog_idle_timeout":"15m","threads":8}`)
	if err != nil {
		t.Fatalf("parseSettings: %v", err)
	}
	if len(s) != 2 {
		t.Errorf("got %d keys, want 2", len(s))
	}
}

func TestParseSettingsRejectsSensitiveKeys(t *testing.T) {
	for _, key := range []string{"api_keys", "csrf", "cors", "cors_allow_origins", "galleries", "autoload_galleries", "p2p_token", "chats_host_exec"} {
		raw, _ := json.Marshal(map[string]any{key: "x"})
		if _, err := parseSettings(string(raw)); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("key %q: error = %v, want rejection naming the key", key, err)
		}
	}
}

func TestParseSettingsRequiresObject(t *testing.T) {
	for _, raw := range []string{`[]`, `"threads"`, `null`, `{`, ``} {
		if _, err := parseSettings(raw); err == nil {
			t.Errorf("parseSettings(%q) succeeded, want an error", raw)
		}
	}
}

func TestObserveMissingUntilApplied(t *testing.T) {
	e := fakeLocalAI(t, http.StatusOK, nil)
	cr := settingsCR(`{"threads":8}`)
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Error("a never-applied Settings should report not existing, so Create posts it")
	}
}

func TestCreatePostsSettingsAndRecordsHash(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	var gotBody map[string]any
	e := fakeLocalAI(t, http.StatusOK, func(r *http.Request, body []byte) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
	})
	cr := settingsCR(`{"threads":8,"watchdog_idle_timeout":"15m"}`)
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/settings" {
		t.Errorf("request = %s %s, want POST /api/settings", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want the bearer key", gotAuth)
	}
	if len(gotBody) != 2 || gotBody["threads"] != float64(8) {
		t.Errorf("body = %v, want exactly the two settings given", gotBody)
	}
	if cr.Status.AtProvider.AppliedHash == "" {
		t.Error("Create did not record the applied hash")
	}
}

func TestObserveDetectsSettingsChange(t *testing.T) {
	e := fakeLocalAI(t, http.StatusOK, nil)
	cr := settingsCR(`{"threads":8}`)
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("after apply, observation = %+v, want exists and up to date", obs)
	}

	cr.Spec.ForProvider.Settings = `{"threads":16}`
	obs, err = e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe after edit: %v", err)
	}
	if obs.ResourceUpToDate {
		t.Error("an edited setting did not mark the resource out of date")
	}
}

func TestCreateRejectedByLocalAIDoesNotRecordHash(t *testing.T) {
	e := fakeLocalAI(t, http.StatusBadRequest, nil)
	cr := settingsCR(`{"threads":8}`)
	if _, err := e.Create(context.Background(), cr); err == nil {
		t.Fatal("Create succeeded although LocalAI returned 400")
	}
	if cr.Status.AtProvider.AppliedHash != "" {
		t.Error("a rejected post was recorded as applied")
	}
}

func TestDeleteLeavesLocalAIAlone(t *testing.T) {
	var requests int
	e := fakeLocalAI(t, http.StatusOK, func(*http.Request, []byte) { requests++ })
	if _, err := e.Delete(context.Background(), settingsCR(`{"threads":8}`)); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if requests != 0 {
		t.Errorf("Delete sent %d request(s), want 0", requests)
	}
}
