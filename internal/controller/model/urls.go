package model

import (
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// checkConfigURLs rejects a Model configuration that references an http or
// https URL whose host is not in allowed. LocalAI fetches download URLs and
// sends proxy requests to upstreams, so a Model must not choose those
// destinations itself. Each allowed entry is a host, or host:port.
func checkConfigURLs(cfg any, allowed []string) error {
	var rejected []string
	walkURLs(cfg, func(raw string) {
		u, err := url.Parse(raw)
		if err != nil || !hostAllowed(u, allowed) {
			rejected = append(rejected, raw)
		}
	})
	if len(rejected) > 0 {
		return errors.Errorf("model configuration references URLs whose host is not in spec.allowedModelUpstreams: %s", strings.Join(rejected, ", "))
	}
	return nil
}

// walkURLs calls fn for every string in v that begins with http:// or https://.
func walkURLs(v any, fn func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			walkURLs(x, fn)
		}
	case []any:
		for _, x := range t {
			walkURLs(x, fn)
		}
	case string:
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			fn(t)
		}
	}
}

// hostAllowed reports whether u names a host in allowed. An entry with a port
// must match host and port; an entry without one matches the host on any port.
func hostAllowed(u *url.URL, allowed []string) bool {
	if u.Hostname() == "" {
		return false
	}
	for _, a := range allowed {
		if a == "" {
			continue
		}
		if strings.EqualFold(a, u.Host) {
			return true
		}
		if !strings.Contains(a, ":") && strings.EqualFold(a, u.Hostname()) {
			return true
		}
	}
	return false
}
