package model

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckConfigURLs(t *testing.T) {
	proxy := map[string]any{"proxy": map[string]any{"upstream_url": "https://opencode.ai/inference/openai"}}
	cases := []struct {
		name    string
		cfg     any
		allowed []string
		wantErr bool
	}{
		{"no URLs needs no allow-list", map[string]any{"name": "m", "model": "qwen.gguf"}, nil, false},
		{"allowed upstream host", proxy, []string{"opencode.ai"}, false},
		{"upstream host not allowed", proxy, []string{"example.org"}, true},
		{"empty allow-list rejects any URL", proxy, nil, true},
		{"host with port must match the port", map[string]any{"u": "https://h.example:8443/x"}, []string{"h.example:443"}, true},
		{"host with port matches exact port", map[string]any{"u": "https://h.example:8443/x"}, []string{"h.example:8443"}, false},
		{"host without port matches any port", map[string]any{"u": "https://h.example:8443/x"}, []string{"h.example"}, false},
		{"nested download in a list is checked", map[string]any{
			"download_files": []any{map[string]any{"uri": "https://evil.example/model.bin"}},
		}, []string{"opencode.ai"}, true},
		{"malformed URL is rejected", map[string]any{"u": "https://[::1"}, []string{"opencode.ai"}, true},
		{"URL with empty host is rejected", map[string]any{"u": "https:///x"}, []string{"opencode.ai"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkConfigURLs(c.cfg, c.allowed)
			if (err != nil) != c.wantErr {
				t.Fatalf("checkConfigURLs error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestCreateRejectsDisallowedUpstreamBeforeImport(t *testing.T) {
	var requests int
	e, _ := testExternal(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.WriteString(w, `{"success":true}`)
	})
	e.allowedHosts = []string{"opencode.ai"}
	cr := newModelCR("zen-qwen3.8-flash")
	cr.Spec.ForProvider.Config = `
name: zen-qwen3.8-flash
proxy:
  upstream_url: https://attacker.example/steal
  api_key_env: OPENCODE_API_KEY
`
	_, err := e.Create(context.Background(), cr)
	if err == nil || !strings.Contains(err.Error(), "attacker.example") {
		t.Fatalf("Create error = %v, want rejection naming the upstream host", err)
	}
	if requests != 0 {
		t.Errorf("sent %d request(s) to LocalAI for a rejected Model, want 0", requests)
	}
}
