package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	accuratev2 "github.com/cybozu-go/accurate/api/accurate/v2"
	uresourcequota "github.com/cybozu-go/accurate/internal/util/resourcequota"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	finalizerName = "clusterresourcequota.accurate.cybozu.com/finalizer"
)

// ClusterResourceQuotaReconciler reconciles a ClusterResourceQuota object
type ClusterResourceQuotaReconciler struct {
	client.Client
}

//+kubebuilder:rbac:groups=accurate.cybozu.com,resources=clusterresourcequotas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=accurate.cybozu.com,resources=clusterresourcequotas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=create;update;patch
//+kubebuilder:rbac:groups=accurate.cybozu.com,resources=clusterresourcequotas/finalizers,verbs=update

// Reconcile implements reconcile.Reconciler interface.
func (r *ClusterResourceQuotaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	crq := &accuratev2.ClusterResourceQuota{}
	if err := r.Get(ctx, req.NamespacedName, crq); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !crq.DeletionTimestamp.IsZero() {
		logger.Info("starting finalization")
		if controllerutil.ContainsFinalizer(crq, finalizerName) {
			if err := r.finalize(ctx, crq); err != nil {
				logger.Error(err, "failed to finalize ClusterResourceQuota")
				return ctrl.Result{}, fmt.Errorf("failed to finalize: %w", err)
			}
			controllerutil.RemoveFinalizer(crq, finalizerName)
			if err := r.Update(ctx, crq); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("finished finalization")
		return ctrl.Result{}, nil
	} else {
		if !controllerutil.ContainsFinalizer(crq, finalizerName) {
			controllerutil.AddFinalizer(crq, finalizerName)
			if err := r.Update(ctx, crq); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	if err := r.reconcileClusterResourceQuota(ctx, crq); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ClusterResourceQuotaReconciler) finalize(ctx context.Context, crq *accuratev2.ClusterResourceQuota) error {
	nsSelector, err := metav1.LabelSelectorAsSelector(&crq.Spec.NamespaceSelector)
	if err != nil {
		return fmt.Errorf("invalid label selector: %w", err)
	}
	var namespaces corev1.NamespaceList
	if err := r.List(ctx, &namespaces, &client.ListOptions{
		LabelSelector: nsSelector,
	}); err != nil {
		return fmt.Errorf("invalid label selector: %w", err)
	}
	for _, ns := range namespaces.Items {
		if err := DeleteOwner(ctx, r, ns.Name, crq.Name); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterResourceQuotaReconciler) reconcileClusterResourceQuota(ctx context.Context, crq *accuratev2.ClusterResourceQuota) error {
	logger := log.FromContext(ctx)

	// Need update spec
	if !uresourcequota.Equals(crq.Spec.Quota.Hard, crq.Status.Hard) {
		logger.Info("ClusterResourceQuota spec updated, write singleton ResourceQuota.", "name", crq.Name, "spec", crq.Spec.Quota.Hard)
		// Update matching NamespaceResourceQuota
		nsSelector, err := metav1.LabelSelectorAsSelector(&crq.Spec.NamespaceSelector)
		if err != nil {
			return fmt.Errorf("invalid label selector: %w", err)
		}
		var namespaces corev1.NamespaceList
		if err := r.List(ctx, &namespaces, &client.ListOptions{
			LabelSelector: nsSelector,
		}); err != nil {
			return fmt.Errorf("invalid label selector: %w", err)
		}
		for _, ns := range namespaces.Items {
			WriteResourceQuota(ctx, r, ns, crq.Name, crq.Spec.Quota.Hard)
		}
		// Update Status
		crq.Status.Hard = crq.Spec.Quota.Hard
		if err := r.Status().Update(ctx, crq); err != nil {
			return fmt.Errorf("failed to update ClusterResourceQuota status: %w", err)
		}
	}

	if err := SyncUsedResource(ctx, r, *crq); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterResourceQuotaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	namespaceHandler := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		crqs := &accuratev2.ClusterResourceQuotaList{}
		mgr.GetCache().List(ctx, crqs)
		requests := []reconcile.Request{}
		for _, crq := range crqs.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crq.Name,
				},
			})
		}
		return requests
	})

	resourceQuotaHnadler := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		rq := o.(*corev1.ResourceQuota)
		if rq.Name != ResourceQuotaSingleton {
			return nil
		}

		nn := types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}
		rq = &corev1.ResourceQuota{}
		if err := r.Get(ctx, nn, rq); err != nil {
			return nil
		}
		var rqOwners []string
		err := json.Unmarshal([]byte(rq.Annotations[SingletonOwnerListAnnotation]), &rqOwners)
		if err != nil {
			// TODO: Sync All ClusterResourceQuota
			return nil
		}

		requests := []reconcile.Request{}
		for _, owner := range rqOwners {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: owner,
				},
			})
		}
		return requests
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&accuratev2.ClusterResourceQuota{}).
		Watches(&corev1.ResourceQuota{}, resourceQuotaHnadler).
		Watches(&corev1.Namespace{}, namespaceHandler).
		Complete(r)
}
