package project

import (
	"context"

	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"
	projectv1beta1 "github.com/rossigee/provider-localai/apis/project/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
	"github.com/rossigee/provider-localai/internal/controller/shared"
)

const (
	errNotProject    = "managed resource is not a Project"
	errGetProject    = "cannot get LocalAI project"
	errCreateProject = "cannot create LocalAI project"
	errUpdateProject = "cannot update LocalAI project"
	errDeleteProject = "cannot delete LocalAI project"

	controllerName = "projects.project.localai.m.crossplane.io"
)

// Setup registers the Project controller.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	opts := append(shared.ReconcilerOptions(o, "Project"),
		managed.WithExternalConnector(&connector{kube: mgr.GetClient()}),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorder(controllerName))),
	)
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(projectv1beta1.SchemeGroupVersion.WithKind("Project")),
		opts...,
	)
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		WithOptions(o.ForControllerRuntime()).
		For(&projectv1beta1.Project{}).
		Complete(r)
}

type connector struct{ kube client.Client }

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*projectv1beta1.Project)
	if !ok {
		return nil, errors.New(errNotProject)
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

func toClientDefaults(d *projectv1beta1.ProjectDefaults) *laiclient.ProjectDefaults {
	if d == nil {
		return nil
	}
	return &laiclient.ProjectDefaults{
		Skills:      d.Skills,
		Addons:      d.Addons,
		Connectors:  d.Connectors,
		MCPServers:  d.MCPServers,
		Collections: d.Collections,
	}
}

func toClientAttached(a *projectv1beta1.ProjectAttached) *laiclient.ProjectAttached {
	if a == nil {
		return nil
	}
	return &laiclient.ProjectAttached{
		Skills:      a.Skills,
		Addons:      a.Addons,
		Connectors:  a.Connectors,
		MCPServers:  a.MCPServers,
		Collections: a.Collections,
	}
}

func toClientAccess(a *projectv1beta1.ProjectAccess) *laiclient.ProjectAccess {
	if a == nil {
		return nil
	}
	out := &laiclient.ProjectAccess{Groups: make([]laiclient.ProjectAccessGrant, 0, len(a.Groups))}
	for _, g := range a.Groups {
		out.Groups = append(out.Groups, laiclient.ProjectAccessGrant{Name: g.Name, Mode: g.Mode})
	}
	return out
}

func fromClientProject(p *laiclient.Project) *projectv1beta1.ProjectObservation {
	if p == nil {
		return nil
	}
	return &projectv1beta1.ProjectObservation{
		ID:        p.ID,
		Name:      p.Name,
		Owner:     p.Owner,
		Status:    p.Status,
		ChatCount: p.ChatCount,
	}
}

// Observe checks that the project exists. Uses external-name (LocalAI project ID)
// when set; falls back to name lookup for adoption of pre-existing projects.
func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr := mg.(*projectv1beta1.Project)
	p := cr.Spec.ForProvider
	id := meta.GetExternalName(cr)
	var proj *laiclient.Project
	var err error
	if id != "" {
		proj, err = e.client.GetProject(ctx, id, p.Owner)
		if err != nil {
			if !laiclient.IsNotFound(err) {
				return managed.ExternalObservation{}, errors.Wrap(err, errGetProject)
			}
			// Crossplane's default NameAsExternalName initializer writes the
			// managed resource's name to the external-name annotation when it
			// is empty. For Project that name is a display name/slug, but
			// LocalAI's GET /api/chats/projects/<id> resolves UUIDs only and
			// returns 404 for a slug. Without a name fallback Observe would
			// report "does not exist" and Create would make a duplicate on
			// every reconcile. Try to adopt by name before concluding the
			// project is gone.
			proj, err = e.findProjectByName(ctx, p.Name, p.Owner)
			if err != nil {
				return managed.ExternalObservation{}, errors.Wrap(err, errGetProject)
			}
			if proj == nil {
				return managed.ExternalObservation{ResourceExists: false}, nil
			}
			meta.SetExternalName(cr, proj.ID)
		}
	} else {
		proj, err = e.findProjectByName(ctx, p.Name, p.Owner)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errGetProject)
		}
		if proj == nil {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		meta.SetExternalName(cr, proj.ID)
	}
	cr.Status.AtProvider = *fromClientProject(proj)
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: projectUpToDate(p, proj),
	}, nil
}

func (e *external) findProjectByName(ctx context.Context, name, owner string) (*laiclient.Project, error) {
	projects, err := e.client.ListProjects(ctx, owner)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i], nil
		}
	}
	return nil, nil
}

func projectUpToDate(p projectv1beta1.ProjectParameters, proj *laiclient.Project) bool {
	if p.Name != proj.Name {
		return false
	}
	if p.Description != "" && p.Description != proj.Description {
		return false
	}
	if !projectToolsUpToDate(p.Defaults, proj.Defaults) ||
		!projectAttachedUpToDate(p.Attached, proj.Attached) {
		return false
	}
	if p.Access != nil {
		want := map[string]string{}
		for _, g := range p.Access.Groups {
			m := g.Mode
			if m == "" {
				m = "read"
			}
			want[g.Name] = m
		}
		got := map[string]string{}
		if proj.Access != nil {
			for _, g := range proj.Access.Groups {
				m := g.Mode
				if m == "" {
					m = "read"
				}
				got[g.Name] = m
			}
		}
		if len(want) != len(got) {
			return false
		}
		for k, v := range want {
			if got[k] != v {
				return false
			}
		}
	}
	return true
}

// projectToolsUpToDate compares the new-chat tool defaults. Without this,
// Update sends defaults on other drift but a manifest-only change to the
// defaults is never detected, so projects keep starting chats with every
// skill and connector the user can see. Lists are compared as sets: LocalAI
// may reorder them and order has no meaning here.
func projectToolsUpToDate(want *projectv1beta1.ProjectDefaults, got *laiclient.ProjectDefaults) bool {
	if want == nil {
		return true // no override requested; do not fight the instance
	}
	if got == nil {
		return len(want.Skills)+len(want.Addons)+len(want.Connectors)+
			len(want.MCPServers)+len(want.Collections) == 0
	}
	return toolListsEqual(want.Skills, got.Skills) &&
		toolListsEqual(want.Addons, got.Addons) &&
		toolListsEqual(want.Connectors, got.Connectors) &&
		toolListsEqual(want.MCPServers, got.MCPServers) &&
		toolListsEqual(want.Collections, got.Collections)
}

// projectAttachedUpToDate compares the always-on tool lists for the same
// reason as projectToolsUpToDate.
func projectAttachedUpToDate(want *projectv1beta1.ProjectAttached, got *laiclient.ProjectAttached) bool {
	if want == nil {
		return true
	}
	if got == nil {
		return len(want.Skills)+len(want.Addons)+len(want.Connectors)+
			len(want.MCPServers)+len(want.Collections) == 0
	}
	return toolListsEqual(want.Skills, got.Skills) &&
		toolListsEqual(want.Addons, got.Addons) &&
		toolListsEqual(want.Connectors, got.Connectors) &&
		toolListsEqual(want.MCPServers, got.MCPServers) &&
		toolListsEqual(want.Collections, got.Collections)
}

// toolListsEqual compares two tool-name lists ignoring order.
func toolListsEqual(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	set := make(map[string]struct{}, len(got))
	for _, v := range got {
		set[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

// Create makes the project.
func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr := mg.(*projectv1beta1.Project)
	p := cr.Spec.ForProvider
	proj, err := e.client.CreateProject(ctx, laiclient.ProjectWrite{
		Name:        p.Name,
		Description: p.Description,
		Defaults:    toClientDefaults(p.Defaults),
		Attached:    toClientAttached(p.Attached),
		Access:      toClientAccess(p.Access),
	}, p.Owner)
	if err != nil {
		// If the project already exists by name, adopt it instead of creating duplicates.
		existing, lerr := e.findProjectByName(ctx, p.Name, p.Owner)
		if lerr == nil && existing != nil {
			meta.SetExternalName(cr, existing.ID)
			cr.Status.AtProvider = *fromClientProject(existing)
			return managed.ExternalCreation{}, nil
		}
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateProject)
	}
	meta.SetExternalName(cr, proj.ID)
	cr.Status.AtProvider = *fromClientProject(proj)
	// Handle archive if requested
	if p.Archived != nil && *p.Archived {
		if err := e.client.ArchiveProject(ctx, proj.ID, p.Owner); err != nil {
			return managed.ExternalCreation{}, errors.Wrap(err, errUpdateProject)
		}
	}
	return managed.ExternalCreation{}, nil
}

// Update updates the project.
func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr := mg.(*projectv1beta1.Project)
	p := cr.Spec.ForProvider
	id := cr.Status.AtProvider.ID
	if id == "" {
		id = p.Name
	}
	_, err := e.client.UpdateProject(ctx, id, laiclient.ProjectWrite{
		Name:        p.Name,
		Description: p.Description,
		Defaults:    toClientDefaults(p.Defaults),
		Attached:    toClientAttached(p.Attached),
		Access:      toClientAccess(p.Access),
	}, p.Owner)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateProject)
	}
	// Handle archive state
	if p.Archived != nil {
		if *p.Archived {
			if err := e.client.ArchiveProject(ctx, id, p.Owner); err != nil {
				return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateProject)
			}
		} else {
			if err := e.client.RestoreProject(ctx, id, p.Owner); err != nil {
				return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateProject)
			}
		}
	}
	return managed.ExternalUpdate{}, nil
}

// Delete deletes the project.
func (e *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr := mg.(*projectv1beta1.Project)
	id := cr.Status.AtProvider.ID
	if id == "" {
		id = cr.Spec.ForProvider.Name
	}
	if err := e.client.DeleteProject(ctx, id, cr.Spec.ForProvider.Owner); err != nil && !laiclient.IsNotFound(err) {
		return managed.ExternalDelete{}, errors.Wrap(err, errDeleteProject)
	}
	return managed.ExternalDelete{}, nil
}
