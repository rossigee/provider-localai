package model

import (
	"context"
	"io"
	"net/http"
	"testing"

	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
)

// stateServer answers the model list and capabilities, and records toggle PUTs.
func stateServer(t *testing.T, disabled, pinned bool, puts *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"zen-qwen3.8-flash","object":"model"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/models/capabilities":
			_, _ = io.WriteString(w, `{"data":[{"id":"zen-qwen3.8-flash","disabled":`+boolJSON(disabled)+`,"pinned":`+boolJSON(pinned)+`}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/models/import":
			_, _ = io.WriteString(w, `{"success":true,"message":"Model configuration created successfully","filename":"zen-qwen3.8-flash.yaml"}`)
		case r.Method == http.MethodPut && puts != nil:
			*puts = append(*puts, r.URL.Path)
			_, _ = io.WriteString(w, `{"success":true}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestObserveIgnoresUnsetStateFlags(t *testing.T) {
	var requests int
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/api/models/capabilities" {
			t.Errorf("capabilities requested although no state flag is set")
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"zen-qwen3.8-flash","object":"model"}]}`)
	})
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	cr.Status.AtProvider.ConfigHash = "" // adopt: baseline only
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want up to date with no managed flags", obs)
	}
}

func TestObserveDetectsEnabledDrift(t *testing.T) {
	e, _ := testExternal(t, stateServer(t, true, false, nil))
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	cr.Status.AtProvider.ConfigHash = modelHashFor(t, cr)
	enabled := true
	cr.Spec.ForProvider.Enabled = &enabled
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want exists but drifted (spec enabled, LocalAI disabled)", obs)
	}
}

func TestObserveMatchingStateIsUpToDate(t *testing.T) {
	e, _ := testExternal(t, stateServer(t, false, true, nil))
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	cr.Status.AtProvider.ConfigHash = modelHashFor(t, cr)
	enabled, pinned := true, true
	cr.Spec.ForProvider.Enabled = &enabled
	cr.Spec.ForProvider.Pinned = &pinned
	obs, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceUpToDate {
		t.Errorf("observe = %+v, want up to date when the flags match", obs)
	}
}

func TestUpdateAppliesDifferingFlags(t *testing.T) {
	var puts []string
	e, _ := testExternal(t, stateServer(t, true, false, &puts))
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	enabled, pinned := true, true
	cr.Spec.ForProvider.Enabled = &enabled
	cr.Spec.ForProvider.Pinned = &pinned
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := map[string]bool{
		"/models/toggle-state/zen-qwen3.8-flash/enable": false,
		"/models/toggle-pinned/zen-qwen3.8-flash/pin":   false,
	}
	for _, p := range puts {
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected toggle %s", p)
		}
		want[p] = true
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("toggle %s was not sent", p)
		}
	}
}

func TestUpdateLeavesMatchingFlagsAlone(t *testing.T) {
	var puts []string
	e, _ := testExternal(t, stateServer(t, false, true, &puts))
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = goodConfig
	enabled, pinned := true, true
	cr.Spec.ForProvider.Enabled = &enabled
	cr.Spec.ForProvider.Pinned = &pinned
	if _, err := e.Update(context.Background(), cr); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(puts) != 0 {
		t.Errorf("toggles %v sent although the flags already match", puts)
	}
}

// modelHashFor is the config hash the controller records for the CR's spec.
func modelHashFor(t *testing.T, cr *modelv1beta1.Model) string {
	t.Helper()
	desired, err := desiredModelConfig(cr)
	if err != nil {
		t.Fatalf("desiredModelConfig: %v", err)
	}
	h, err := modelConfigHash(desired)
	if err != nil {
		t.Fatalf("modelConfigHash: %v", err)
	}
	return h
}
