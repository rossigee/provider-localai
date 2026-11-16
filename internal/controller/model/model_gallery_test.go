package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
)

func galleryCR(name string) *modelv1beta1.Model {
	cr := newModelCR(name)
	cr.Spec.ForProvider.Gallery = &modelv1beta1.GalleryInstall{ID: "qwen3-8b", Variant: "q4"}
	return cr
}

func TestCreateGalleryInstallsWithoutImport(t *testing.T) {
	var applyBody map[string]any
	var imports int
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/models/apply":
			_ = json.NewDecoder(r.Body).Decode(&applyBody)
			_, _ = io.WriteString(w, `{"id":"job-1","status_url":"https://localai.example/models/jobs/job-1"}`)
		case r.URL.Path == "/models/import":
			imports++
			_, _ = io.WriteString(w, `{"success":true}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := galleryCR("zen")
	if _, err := e.Create(context.Background(), cr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if imports != 0 {
		t.Errorf("config import sent %d time(s) for a gallery model, want 0", imports)
	}
	if applyBody["id"] != "qwen3-8b" || applyBody["variant"] != "q4" || applyBody["name"] != "zen" {
		t.Errorf("apply body = %v, want id, variant and the Model's name", applyBody)
	}
	if cr.Status.AtProvider.PendingImportUUID != "job-1" {
		t.Errorf("pending job = %q, want job-1", cr.Status.AtProvider.PendingImportUUID)
	}
}

func TestCreateRejectsGalleryWithConfig(t *testing.T) {
	var requests int
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
	})
	cr := galleryCR("zen")
	cr.Spec.ForProvider.Config = goodConfig
	if _, err := e.Create(context.Background(), cr); err == nil {
		t.Fatal("Create accepted gallery and config together")
	}
	if requests != 0 {
		t.Errorf("sent %d request(s) before rejecting the spec, want 0", requests)
	}
}

func TestObserveReportsFailedGalleryJob(t *testing.T) {
	var cleared bool
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
		case "/models/jobs/job-1":
			_, _ = io.WriteString(w, `{"processed":true,"error":"disk full"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := galleryCR("zen")
	cr.Status.AtProvider.PendingImportUUID = "job-1"
	cr.Status.AtProvider.PendingImportSince = time.Now().UTC().Format(time.RFC3339)
	if _, err := e.Observe(context.Background(), cr); err == nil {
		t.Fatal("Observe did not report the failed install")
	}
	cleared = cr.Status.AtProvider.PendingImportUUID == ""
	if !cleared {
		t.Error("failed job was not cleared, so the next reconcile would not reinstall")
	}
}

func TestObserveWaitsForRunningGalleryJob(t *testing.T) {
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
		case "/models/jobs/job-1":
			_, _ = io.WriteString(w, `{"processed":false,"progress":40}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	cr := galleryCR("zen")
	cr.Status.AtProvider.PendingImportUUID = "job-1"
	cr.Status.AtProvider.PendingImportSince = time.Now().UTC().Format(time.RFC3339)
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want the running install treated as in progress", obs)
	}
}
