/*
Copyright 2024 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cmd/provider is a single 264-statement main() that runs flag parsing and then
// blocks on a Kubernetes API server, so it has no unit-testable seam. Driving
// the built binary is the honest alternative: it covers the flag surface and
// the parse-before-connect ordering, which is where a mistake would actually
// bite - a mistyped flag name silently ignored, a default that does not match
// the documented value, or validation that runs after the cluster is required.
//
// Everything past flag parsing needs a live cluster and is out of scope here.
//
// One limit worth stating plainly: these tests prove a flag is accepted, given
// the right type, defaulted as documented, and reported at startup. They cannot
// prove the parsed value is then *used*. Replacing `*maxReconcileRate` in the
// LimitRESTConfig call with a literal still passes, because the startup log
// prints the flag rather than the value handed to the manager. Verifying
// downstream use would mean extracting a testable run() from main(), which is a
// production refactor rather than a test.
var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "provider-bin")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	binaryPath = filepath.Join(dir, "provider")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binaryPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("cannot build provider binary: " + err.Error())
	}

	os.Exit(m.Run())
}

func runProvider(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running provider failed: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// TestHelpListsEveryFlag pins the documented flag surface. A flag that is
// removed, renamed or given a different default changes what an operator can
// configure at startup, and nothing else in the suite would notice.
func TestHelpListsEveryFlag(t *testing.T) {
	stdout, stderr, code := runProvider(t, "--help")
	out := stdout + stderr

	if code != 0 {
		t.Errorf("--help exit code = %d, want 0", code)
	}

	// Defaults are asserted only for flags kingpin renders them on. Boolean
	// flags print bare, with no "=false", so there is nothing to match there.
	want := map[string]string{
		"--debug":                "",
		"--leader-election":      "",
		"--sync":                 "1h",
		"--poll":                 "1m",
		"--max-reconcile-rate":   "10",
		"--cache-init-timeout":   "5m",
		"--poll-state-metric":    "5s",
		"--metrics-bind-address": ":8080",
	}
	for flag, def := range want {
		if !strings.Contains(out, flag) {
			t.Errorf("--help does not document %s", flag)
			continue
		}
		if def != "" && !strings.Contains(out, def) {
			t.Errorf("--help does not show default %q for %s", def, flag)
		}
	}

	// Short forms are operator-facing too.
	for _, short := range []string{"-d, --debug", "-l, --leader-election", "-s, --sync"} {
		if !strings.Contains(out, short) {
			t.Errorf("--help does not document the short form %q", short)
		}
	}
}

// TestUnknownFlagIsRejected covers the parse-before-connect ordering: a bad flag
// must fail immediately, not after trying to reach a cluster.
func TestUnknownFlagIsRejected(t *testing.T) {
	_, stderr, code := runProvider(t, "--not-a-real-flag")

	if code == 0 {
		t.Error("an unknown flag must be rejected with a non-zero exit code")
	}
	if !strings.Contains(stderr, "not-a-real-flag") {
		t.Errorf("stderr does not name the offending flag:\n%s", stderr)
	}
	if strings.Contains(stderr, "Cannot get API server rest config") {
		t.Error("flag validation must happen before the cluster is contacted")
	}
}

// TestInvalidFlagValueIsRejected covers per-flag parsing rather than just
// flag recognition.
func TestInvalidFlagValueIsRejected(t *testing.T) {
	cases := map[string][]string{
		"non-numeric max-reconcile-rate": {"--max-reconcile-rate", "not-a-number"},
		"unparseable sync duration":      {"--sync", "not-a-duration"},
		"unparseable poll duration":      {"--poll", "5 fortnights"},
		"non-boolean debug":              {"--debug=maybe-not-a-bool"},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := runProvider(t, args...)
			if code == 0 {
				t.Errorf("expected rejection, got exit 0\n%s", stderr)
			}
			if strings.Contains(stderr, "Cannot get API server rest config") {
				t.Errorf("flag validation must happen before the cluster is contacted:\n%s", stderr)
			}
		})
	}
}

// TestValidFlagsParseAndProceedToClusterConnect is the positive counterpart:
// with well-formed flags, parsing succeeds and startup advances to the point
// where it needs a cluster. This is what proves the flags above are not merely
// rejected but actually accepted.
func TestValidFlagsParseAndProceedToClusterConnect(t *testing.T) {
	_, stderr, _ := runProvider(t,
		"--debug",
		"--leader-election",
		"--sync=30m",
		"--poll=45s",
		"--max-reconcile-rate=42",
		"--cache-init-timeout=90s",
		"--poll-state-metric=15s",
		"--metrics-bind-address=:9090",
		"--no-enable-management-policies",
	)

	if !strings.Contains(stderr, "Provider starting up") {
		t.Fatalf("valid flags did not reach startup:\n%s", stderr)
	}
	// The parsed values must appear in the startup log, or the flags are
	// accepted but not wired to anything. The shape below is zap's console
	// encoder, which puts a space after the colon - not the JSON form.
	for _, want := range []string{
		`"sync-interval": "30m0s"`,
		`"poll-interval": "45s"`,
		`"max-reconcile-rate": 42`,
		`"cache-init-timeout": "1m30s"`,
		`"leader-election": true`,
		`"debug-mode": true`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("startup log does not reflect a parsed flag; missing %s\n%s", want, stderr)
		}
	}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}
