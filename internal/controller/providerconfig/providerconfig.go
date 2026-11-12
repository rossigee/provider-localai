package providerconfig

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	laiclient "github.com/rossigee/localai-client/client"

	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

const (
	controllerName = "providerconfig.localai.m.crossplane.io"

	// checkTimeout bounds the authenticated request made to LocalAI.
	checkTimeout = 30 * time.Second
	// healthyRequeue is how often a working ProviderConfig is re-checked.
	healthyRequeue = 5 * time.Minute
	// unhealthyRequeue retries sooner while LocalAI or its credentials are unusable.
	unhealthyRequeue = 1 * time.Minute
)

// Setup registers the ProviderConfig controller.
func Setup(mgr ctrl.Manager) error {
	logger := mgr.GetLogger().WithValues("controller", controllerName)
	logger.Info("Setting up ProviderConfig controller")
	r := &reconciler{kube: mgr.GetClient(), logger: logger}
	c, err := ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&v1beta1.ProviderConfig{}).
		Build(r)
	if err != nil {
		logger.Error(err, "Failed to build controller")
		return err
	}
	logger.Info("Controller built, starting", "controller", c)
	return nil
}

type reconciler struct {
	kube   client.Client
	logger logr.Logger
}

func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	r.logger.Info("Reconciling ProviderConfig", "name", req.Name, "namespace", req.Namespace)
	pc := &v1beta1.ProviderConfig{}
	if err := r.kube.Get(ctx, req.NamespacedName, pc); err != nil {
		r.logger.Error(err, "Failed to get ProviderConfig", "name", req.Name)
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Ready means LocalAI accepted the configured credentials. Agents, Models
	// and Skills wait for this condition before they connect.
	result := reconcile.Result{RequeueAfter: healthyRequeue}
	if err := r.check(ctx, pc); err != nil {
		r.logger.Info("ProviderConfig check failed", "name", pc.GetName(), "error", err.Error())
		pc.Status.SetConditions(xpv1.Unavailable().WithMessage(err.Error()))
		result.RequeueAfter = unhealthyRequeue
	} else {
		pc.Status.SetConditions(xpv1.Available())
	}

	fresh := &v1beta1.ProviderConfig{}
	if err := r.kube.Get(ctx, client.ObjectKey{Name: pc.GetName(), Namespace: pc.GetNamespace()}, fresh); err != nil {
		r.logger.Error(err, "Failed to get fresh ProviderConfig", "name", pc.GetName())
		return reconcile.Result{RequeueAfter: 30 * time.Second}, client.IgnoreNotFound(err)
	}
	fresh.Status = pc.Status
	r.logger.Info("Updating ProviderConfig status", "name", fresh.GetName())
	if err := r.kube.Status().Update(ctx, fresh); err != nil {
		r.logger.Error(err, "Failed to update ProviderConfig status", "name", fresh.GetName())
		if !errors.IsConflict(err) {
			return reconcile.Result{RequeueAfter: 30 * time.Second}, err
		}
	}
	return result, nil
}

// check makes an authenticated request to LocalAI with the ProviderConfig's
// credentials. It succeeds only when LocalAI answers with a 2xx status.
func (r *reconciler) check(ctx context.Context, pc *v1beta1.ProviderConfig) error {
	cfg, err := clients.GetLocalAIConfig(ctx, pc, r.kube)
	if err != nil {
		return err
	}
	laic, err := laiclient.New(ctx, cfg)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	_, err = laic.ListModels(cctx)
	return err
}
