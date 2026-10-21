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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Every controller calls StartSpan on each reconcile, so it has to tolerate the
// two inputs it cannot control: a context that has not been threaded through
// yet, and no tracer configured.
func TestStartSpanToleratesNilContext(t *testing.T) {
	ctx, span := StartSpan(nil, "test") //nolint:staticcheck
	if ctx == nil {
		t.Fatal("StartSpan must return a usable context when given nil")
	}
	if span == nil {
		t.Fatal("StartSpan must return a non-nil span handle")
	}
	span.End()
}

func TestStartSpanWithAttrsToleratesNilContext(t *testing.T) {
	ctx, span := StartSpanWithAttrs(nil, "test", "Kind", "name", "op") //nolint:staticcheck
	if ctx == nil {
		t.Fatal("StartSpanWithAttrs must return a usable context when given nil")
	}
	if span == nil {
		t.Fatal("StartSpanWithAttrs must return a non-nil span handle")
	}
	span.End()
}

func TestStartSpanPropagatesContext(t *testing.T) {
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)

	out, span := StartSpan(ctx, "child")
	if out == nil {
		t.Fatal("StartSpan must return a context")
	}
	span.End()

	if trace.SpanContextFromContext(out).TraceID() != parent.TraceID() {
		t.Error("StartSpan must keep the caller's trace, or spans from one reconcile scatter across traces")
	}
}

func TestStartSpanAttachesAttributes(t *testing.T) {
	attrs := SpanAttrs("ClientRoleMapping", "my-resource", "observe")

	_, span := StartSpan(context.Background(), "test", attrs...)
	if span == nil {
		t.Fatal("StartSpan returned a nil span")
	}
	span.End()
}

// TestSpanAttrsUsesTheExpectedKeys pins the attribute key strings, since they
// are the contract a backend queries against.
//
// The keys are spelled out as literals rather than referenced through the
// constants on purpose: a test that reads the same constant it is checking
// cannot detect that constant changing value, which is the actual risk here.
func TestSpanAttrsUsesTheExpectedKeys(t *testing.T) {
	got := SpanAttrs("Kind", "name", "op")
	want := map[string]string{
		"crossplane.resource.type": "Kind",
		"crossplane.resource.name": "name",
		"crossplane.operation":     "op",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d attributes, want %d: %v", len(got), len(want), got)
	}
	for _, kv := range got {
		exp, ok := want[string(kv.Key)]
		if !ok {
			t.Errorf("unexpected attribute key %q", kv.Key)
			continue
		}
		if kv.Value.AsString() != exp {
			t.Errorf("%s = %q, want %q", kv.Key, kv.Value.AsString(), exp)
		}
	}
}

func TestGetEnvPrefersTheEnvironment(t *testing.T) {
	const key = "PROVIDER_TRACING_TEST_KEY"
	if got := getEnv(key, "fallback"); got != "fallback" {
		t.Errorf("getEnv with the variable unset = %q, want the default", got)
	}
	t.Setenv(key, "from-env")
	if got := getEnv(key, "fallback"); got != "from-env" {
		t.Errorf("getEnv = %q, want from-env", got)
	}
	t.Setenv(key, "")
	if got := getEnv(key, "fallback"); got != "fallback" {
		t.Errorf("getEnv with an empty value = %q, want the default", got)
	}
}

func TestInitIsSafeWithoutAnExporter(t *testing.T) {
	// Init must not require a collector to be reachable, or the provider would
	// fail to start when tracing is misconfigured.
	shutdown := Init("test-service")
	if shutdown == nil {
		t.Fatal("Init must return a shutdown function")
	}
	shutdown(context.Background())
}

func TestSpanAttrsValuesAreStrings(t *testing.T) {
	for _, kv := range SpanAttrs("a", "b", "c") {
		if kv.Value.Type() != attribute.STRING {
			t.Errorf("attribute %s has type %v, want STRING", kv.Key, kv.Value.Type())
		}
	}
}
