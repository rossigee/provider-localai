package collection

import (
	"context"
	"slices"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"
	collectionv1beta1 "github.com/rossigee/provider-localai/apis/collection/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotCollection    = "managed resource is not a Collection"
	errGetCollection    = "cannot get LocalAI collection"
	errCreateCollection = "cannot create LocalAI collection"
	errSources          = "cannot update the sources of the LocalAI collection"
	errDeleteSources    = "cannot stop the sources of the LocalAI collection"

	controllerName = "collections.collection.localai.m.crossplane.io"
)

// Setup registers the Collection controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Collection"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(collectionv1beta1.SchemeGroupVersion.WithKind("Collection")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&collectionv1beta1.Collection{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*collectionv1beta1.Collection)
	if !ok {
		return nil, errors.New(errNotCollection)
	}
	pc, err := clients.GetProviderConfig(ctx, c.kube, cr.GetNamespace(), cr.GetProviderConfigReference())
	if err != nil {
		return nil, err
	}
	if pc.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionTrue {
		return nil, errors.New("provider is not ready")
	}
	cfg, err := clients.GetLocalAIConfig(ctx, pc, c.kube)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI client config")
	}
	laic, err := laiclient.New(ctx, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build LocalAI client")
	}
	return &external{client: laic}, nil
}

type external struct{ client *laiclient.Client }

func (e *external) Disconnect(_ context.Context) error { return nil }

// Observe checks that the collection exists and that the URLs it follows
// match the spec.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*collectionv1beta1.Collection)
	p := cr.Spec.ForProvider
	names, err := e.client.ListCollections(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errGetCollection)
	}
	if !slices.Contains(names, p.Name) {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	have, err := e.client.ListCollectionSources(ctx, p.Name)
	if err != nil {
		if laiclient.IsNotFound(err) {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errGetCollection)
	}
	cr.Status.AtProvider.Name = p.Name
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: sourcesMatch(p.Sources, have),
	}, nil
}

// sourcesMatch reports whether the followed URLs are exactly the spec's. An
// interval is compared only when the spec sets one.
func sourcesMatch(want []collectionv1beta1.CollectionSource, have []laiclient.CollectionSource) bool {
	if len(want) != len(have) {
		return false
	}
	byURL := make(map[string]laiclient.CollectionSource, len(have))
	for _, h := range have {
		byURL[h.URL] = h
	}
	for _, w := range want {
		h, ok := byURL[w.URL]
		if !ok {
			return false
		}
		if w.UpdateIntervalMinutes != 0 && h.UpdateInterval != w.UpdateIntervalMinutes {
			return false
		}
	}
	return true
}

// Create makes the collection and starts following the spec's URLs.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*collectionv1beta1.Collection)
	p := cr.Spec.ForProvider
	if _, err := e.client.CreateCollection(ctx, p.Name); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateCollection)
	}
	for _, s := range p.Sources {
		if _, err := e.client.AddCollectionSource(ctx, p.Name, s.URL, s.UpdateIntervalMinutes); err != nil {
			return managed.ExternalCreation{}, errors.Wrap(err, errSources)
		}
	}
	return managed.ExternalCreation{}, nil
}

// Update stops the URLs the spec no longer lists, and starts the ones it
// adds. A URL whose interval changed is stopped and started again.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*collectionv1beta1.Collection)
	p := cr.Spec.ForProvider
	have, err := e.client.ListCollectionSources(ctx, p.Name)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errGetCollection)
	}
	want := make(map[string]collectionv1beta1.CollectionSource, len(p.Sources))
	for _, s := range p.Sources {
		want[s.URL] = s
	}
	current := make(map[string]laiclient.CollectionSource, len(have))
	for _, h := range have {
		current[h.URL] = h
		w, ok := want[h.URL]
		if !ok || (w.UpdateIntervalMinutes != 0 && h.UpdateInterval != w.UpdateIntervalMinutes) {
			if _, err := e.client.RemoveCollectionSource(ctx, p.Name, h.URL); err != nil {
				return managed.ExternalUpdate{}, errors.Wrap(err, errSources)
			}
		}
	}
	for _, s := range p.Sources {
		h, ok := current[s.URL]
		if ok && (s.UpdateIntervalMinutes == 0 || h.UpdateInterval == s.UpdateIntervalMinutes) {
			continue
		}
		if _, err := e.client.AddCollectionSource(ctx, p.Name, s.URL, s.UpdateIntervalMinutes); err != nil {
			return managed.ExternalUpdate{}, errors.Wrap(err, errSources)
		}
	}
	return managed.ExternalUpdate{}, nil
}

// Delete stops the URLs the spec lists. LocalAI has no endpoint that deletes a
// collection, so the collection and its entries stay in place.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*collectionv1beta1.Collection)
	p := cr.Spec.ForProvider
	for _, s := range p.Sources {
		if _, err := e.client.RemoveCollectionSource(ctx, p.Name, s.URL); err != nil && !laiclient.IsNotFound(err) {
			return managed.ExternalDelete{}, errors.Wrap(err, errDeleteSources)
		}
	}
	return managed.ExternalDelete{}, nil
}
