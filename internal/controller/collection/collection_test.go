package collection

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	collectionv1beta1 "github.com/rossigee/provider-localai/apis/collection/v1beta1"
)

func collectionCR(sources ...collectionv1beta1.CollectionSource) *collectionv1beta1.Collection {
	return &collectionv1beta1.Collection{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "docs"},
		Spec: collectionv1beta1.CollectionSpec{ForProvider: collectionv1beta1.CollectionParameters{
			Name:    "docs",
			Sources: sources,
		}},
	}
}

func collectionExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

func TestObserveMissingCollectionDoesNotExist(t *testing.T) {
	e := collectionExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"collections":["other"],"count":1}`)
	})
	obs, err := e.Observe(context.Background(), collectionCR())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.ResourceExists {
		t.Errorf("ResourceExists = true, want false when the name is not listed")
	}
}

func TestObserveMatchingSourcesIsUpToDate(t *testing.T) {
	e := collectionExternal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collections":
			_, _ = io.WriteString(w, `{"collections":["docs"],"count":1}`)
		case "/api/collections/docs/sources":
			_, _ = io.WriteString(w, `{"sources":[{"url":"https://example.com/a","update_interval":60,"last_update":""}],"count":1}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	obs, err := e.Observe(context.Background(), collectionCR(collectionv1beta1.CollectionSource{URL: "https://example.com/a", UpdateIntervalMinutes: 60}))
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !obs.ResourceExists || !obs.ResourceUpToDate {
		t.Errorf("observation = %+v, want existing and up to date", obs)
	}
}

func TestSourcesMatchDetectsExtraAndChangedURLs(t *testing.T) {
	have := []laiclient.CollectionSource{{URL: "https://example.com/a", UpdateInterval: 60}}
	if sourcesMatch([]collectionv1beta1.CollectionSource{{URL: "https://example.com/a"}, {URL: "https://example.com/b"}}, have) {
		t.Errorf("a missing URL matched, want drift")
	}
	if sourcesMatch([]collectionv1beta1.CollectionSource{{URL: "https://example.com/a", UpdateIntervalMinutes: 30}}, have) {
		t.Errorf("a changed interval matched, want drift")
	}
	if !sourcesMatch([]collectionv1beta1.CollectionSource{{URL: "https://example.com/a"}}, have) {
		t.Errorf("an unset interval should not be compared")
	}
}

func TestDeleteStopsSpecSourcesAndKeepsCollection(t *testing.T) {
	var removed []string
	e := collectionExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/collections/docs/sources" {
			removed = append(removed, r.URL.Path)
			_, _ = io.WriteString(w, `{"status":"ok"}`)
			return
		}
		t.Errorf("unexpected request %s %s; Delete must not delete the collection", r.Method, r.URL.Path)
	})
	cr := collectionCR(collectionv1beta1.CollectionSource{URL: "https://example.com/a"})
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("removed %d source request(s), want 1", len(removed))
	}
}
