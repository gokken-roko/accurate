package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	accuratev2 "github.com/cybozu-go/accurate/api/accurate/v2"
	uresourcequota "github.com/cybozu-go/accurate/internal/util/resourcequota"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ResourceQuotaSingleton       = "clusterresourcequota.accurate.cybozu.com"
	SingletonOwnerListAnnotation = "clusterresourcequota.accurate.cybozu.com/singleton-owner"
	IgnoreAnnotation             = "clusterresourcequota.accurate.cybozu.com/ignore"
)

// getSingleton retrieves the singleton ResourceQuota for the given namespace.
// - If singleton-resourcequota not exists, it initializes a new one with the singleton name.
//   - Create new ResourceQuota with the returned ResourceQuota object.
func getSingleton(ctx context.Context, r client.Client, namespace string) (*corev1.ResourceQuota, error) {
	nn := types.NamespacedName{Namespace: namespace, Name: ResourceQuotaSingleton}
	rq := &corev1.ResourceQuota{}
	if err := r.Get(ctx, nn, rq); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		rq.Name = ResourceQuotaSingleton
		rq.Namespace = namespace
		// Initialize the Owner List Annotation
		rqOwners := []string{}
		b, _ := json.Marshal(rqOwners)
		rq.Annotations = map[string]string{}
		rq.Annotations[SingletonOwnerListAnnotation] = string(b)
	}
	return rq, nil
}

func createSingleton(ctx context.Context, r client.Client, crqName string, rq *corev1.ResourceQuota) error {
	if rq.CreationTimestamp.IsZero() {
		rqOwners := []string{crqName}
		b, _ := json.Marshal(rqOwners)
		rq.Annotations = map[string]string{}
		rq.Annotations[SingletonOwnerListAnnotation] = string(b)

		if err := r.Create(ctx, rq); err != nil {
			return fmt.Errorf("failed to create singleton ResourceQuota: %w", err)
		}
	}
	return nil
}

func updateSingleton(ctx context.Context, r client.Client, currentRQ *corev1.ResourceQuota, crqName string) error {
	if currentRQ.Annotations == nil {
		currentRQ.Annotations = map[string]string{}
	}
	var rqOwners []string
	err := json.Unmarshal([]byte(currentRQ.Annotations[SingletonOwnerListAnnotation]), &rqOwners)
	if err != nil {
		return fmt.Errorf("failed to unmarshal singleton owners list: %w", err)
	}

	if !slices.Contains(rqOwners, crqName) {
		rqOwners = append(rqOwners, crqName)
	}
	// Add a new singleton-owner
	owners, _ := json.Marshal(rqOwners)
	currentRQ.Annotations[SingletonOwnerListAnnotation] = string(owners)
	min, _ := GetMinResourceList(ctx, r, rqOwners)
	currentRQ.Spec.Hard = min
	if err := r.Update(ctx, currentRQ); err != nil {
		return fmt.Errorf("failed to update singleton ResourceQuota: %w", err)
	}
	return nil
}

func deleteSingleton(ctx context.Context, r client.Client, namespace string) error {
	nn := types.NamespacedName{Namespace: namespace, Name: ResourceQuotaSingleton}
	rq := &corev1.ResourceQuota{}
	if err := r.Get(ctx, nn, rq); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	r.Delete(ctx, rq)
	return nil
}

func DeleteOwner(ctx context.Context, r client.Client, namespace string, crqName string) error {
	logger := log.FromContext(ctx)
	rq, err := getSingleton(ctx, r, namespace)
	if err != nil {
		return fmt.Errorf("failed to get singleton to DeleteOwner %w", err)
	}

	var rqOwners []string
	err = json.Unmarshal([]byte(rq.Annotations[SingletonOwnerListAnnotation]), &rqOwners)
	if err != nil {
		return fmt.Errorf("failed to unmarshal singleton owners list: %w", err)
	}

	logger.Info("Deleting owner from singleton ResourceQuota", "namespace", namespace, "owner", crqName)

	var newOwners []string
	for _, owner := range rqOwners {
		if owner != crqName {
			newOwners = append(newOwners, owner)
		}
	}
	if len(newOwners) == 0 {
		logger.Info("No singleton-owners left, delete singleton ResourceQuota", "namespace", namespace)
		r.Delete(ctx, rq)
		return nil
	}
	owners, _ := json.Marshal(newOwners)
	rq.Annotations[SingletonOwnerListAnnotation] = string(owners)
	if err := r.Update(ctx, rq); err != nil {
		return fmt.Errorf("failed to update singleton ResourceQuota after deleting owner: %w", err)
	}
	return nil
}

func GetMinResourceList(ctx context.Context, r client.Client, crqNames []string) (corev1.ResourceList, error) {
	min := corev1.ResourceList{}
	for _, name := range crqNames {
		var crq accuratev2.ClusterResourceQuota
		if err := r.Get(ctx, client.ObjectKey{Name: name}, &crq); err != nil {
			// TODO: Error log
			continue
		}
		min = uresourcequota.Min(min, crq.Spec.Quota.Hard)
	}
	return min, nil
}

func SyncUsedResource(ctx context.Context, r client.Client, crq accuratev2.ClusterResourceQuota) error {
	logger := log.FromContext(ctx)
	nsSelector, _ := metav1.LabelSelectorAsSelector(&crq.Spec.NamespaceSelector)
	namespaces := corev1.NamespaceList{}
	if err := r.List(ctx, &namespaces, &client.ListOptions{
		LabelSelector: nsSelector,
	}); err != nil {
		return err
	}
	// Sum ResourceUsage in matching namespaces
	usedResource := corev1.ResourceList{}
	for _, ns := range namespaces.Items {
		if ns.Annotations != nil {
			// Delete ResourceQuota if the namespace is ignored
			if ns.Annotations[IgnoreAnnotation] == "true" {
				logger.Info("Namespace is ignored, skipping WriteResourceQuota", "namespace", ns.Name)
				deleteSingleton(ctx, r, ns.Name)
				continue
			}
		}
		quota := corev1.ResourceQuota{}
		// TODO: if not owner, add own
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: ResourceQuotaSingleton}, &quota); err != nil {
			if errors.IsNotFound(err) {
				WriteResourceQuota(ctx, r, ns, crq.Name, crq.Spec.Quota.Hard)
			} else {
				return err
			}
		}
		usedResource = uresourcequota.Add(usedResource, quota.Status.Used)
	}

	logger.Info("Syncing used resource")
	newCrq := crq.DeepCopy()
	newCrq.Status.Used = usedResource
	patch := client.MergeFrom(&crq)
	if err := r.Status().Patch(ctx, newCrq, patch); err != nil {
		return err
	}
	return nil
}

func WriteResourceQuota(ctx context.Context, r client.Client, namespace corev1.Namespace, crqName string, rl corev1.ResourceList) error {
	logger := log.FromContext(ctx)

	if namespace.Annotations != nil {
		if namespace.Annotations[IgnoreAnnotation] == "true" {
			logger.Info("Namespace is ignored, skipping WriteResourceQuota", "namespace", namespace.Name)
			deleteSingleton(ctx, r, namespace.Name)
			return nil
		}
	}

	rq, err := getSingleton(ctx, r, namespace.Name)
	if err != nil {
		return fmt.Errorf("failed to get singleton ResourceQuota: %w", err)
	}

	if rq.CreationTimestamp.IsZero() {
		rq.Spec.Hard = rl
		if err := createSingleton(ctx, r, crqName, rq); err != nil {
			return fmt.Errorf("failed to create singleton: %w", err)
		}
	} else {
		if err := updateSingleton(ctx, r, rq, crqName); err != nil {
			return fmt.Errorf("failed to update singleton: %w", err)
		}
	}
	return nil
}
