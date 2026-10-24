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

	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

const controllerName = "providerconfig.localai.crossplane.io"

// Setup registers the ProviderConfig controller.
func Setup(mgr ctrl.Manager) error {
	r := &reconciler{kube: mgr.GetClient(), logger: mgr.GetLogger()}
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&v1beta1.ProviderConfig{}).
		Complete(r)
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
	r.logger.Info("ProviderConfig found, setting Available condition", "name", pc.GetName())
	pc.Status.SetConditions(xpv1.Available())
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
	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}
