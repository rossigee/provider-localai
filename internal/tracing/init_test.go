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

package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

// Init replaces the global tracer provider and the package-level tp, so these
// tests put both back rather than leaking state into the rest of the package.
func restoreGlobals(t *testing.T) {
	t.Helper()
	prevTP := tp
	prevTracer := tracer
	t.Cleanup(func() {
		tp = prevTP
		tracer = prevTracer
		if prevTP != nil {
			otel.SetTracerProvider(prevTP)
		}
	})
}

// With tracing disabled, Init must hand back a usable no-op shutdown. The
// provider is started with no collector configured, so an error here would stop
// the whole provider from booting.
func TestInitDisabledReturnsANoOpShutdown(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "false")

	shutdown := Init("svc")
	if shutdown == nil {
		t.Fatal("Init must always return a shutdown function")
	}
	// Must not panic, and must be safe to call more than once.
	shutdown(context.Background())
	shutdown(context.Background())

	if tp != nil {
		t.Error("a tracer provider was installed while tracing is disabled")
	}
}

// The enabled path installs a real tracer provider and returns a shutdown that
// flushes it. Creating the gRPC exporter must not require a collector to be
// reachable, since the provider starts long before one necessarily is.
func TestInitEnabledInstallsAProviderAndShutsItDown(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_SAMPLING_RATIO", "0.1")

	shutdown := Init("svc")
	if shutdown == nil {
		t.Fatal("Init must return a shutdown function when enabled")
	}
	if tp == nil {
		t.Error("no tracer provider was installed while tracing is enabled")
	}
	if otel.GetTracerProvider() == nil {
		t.Error("otel.SetTracerProvider was not called")
	}

	shutdown(context.Background())
}

// A malformed sampling ratio must not be fatal: the parse error is swallowed and
// the default kept, so a typo in a value cannot stop the provider starting.
func TestInitToleratesAMalformedSamplingRatio(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_SAMPLING_RATIO", "not-a-number")

	shutdown := Init("svc")
	if shutdown == nil {
		t.Fatal("Init must return a shutdown function even with a bad sampling ratio")
	}
	if tp == nil {
		t.Error("a bad sampling ratio should fall back to the default, not skip provider setup")
	}
	shutdown(context.Background())
}

func TestInitToleratesAnUnparseableEnabledFlag(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "perhaps")

	shutdown := Init("svc")
	if shutdown == nil {
		t.Fatal("Init must return a shutdown function for an unparseable flag")
	}
	// ParseBool errors are ignored and enabled stays false, so nothing is set up.
	if tp != nil {
		t.Error("an unparseable OTEL_TRACING_ENABLED should leave tracing disabled")
	}
	shutdown(context.Background())
}

// The returned shutdown must be idempotent enough to survive being called after
// a failed setup, since tp is package state that a previous Init may have set.
func TestShutdownIsSafeAfterRepeatedInit(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_SAMPLING_RATIO", "0.1")

	first := Init("svc-one")
	second := Init("svc-two")

	if first == nil || second == nil {
		t.Fatal("both Init calls must return a shutdown function")
	}
	second(context.Background())
	first(context.Background())
}
