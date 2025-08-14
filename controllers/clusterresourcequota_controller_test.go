package controllers

import (
	"context"
	"time"

	accuratev2 "github.com/cybozu-go/accurate/api/accurate/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("ClusterResourceQuota controller", func() {
	ctx := context.Background()
	var stopFunc func()

	BeforeEach(func() {
		mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
			Scheme:         scheme,
			LeaderElection: false,
			Metrics:        server.Options{BindAddress: "0"},
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Client: client.Options{
				Cache: &client.CacheOptions{
					Unstructured: true,
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		crqReconciler := &ClusterResourceQuotaReconciler{
			Client: mgr.GetClient(),
		}
		err = crqReconciler.SetupWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())
		ctx, cancel := context.WithCancel(ctx)
		stopFunc = cancel
		go func() {
			err := mgr.Start(ctx)
			if err != nil {
				panic(err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
	})

	AfterEach(func() {
		stopFunc()
		time.Sleep(100 * time.Millisecond)
	})

	It("should create and delete ClusterResourceQuota", func() {
		ns := &corev1.Namespace{}
		ns.Name = "namespace1"
		ns.Labels = map[string]string{
			"team": "namespace1-team",
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		tmpl := &accuratev2.ClusterResourceQuota{}
		tmpl.Name = "namespace1-crq"
		tmpl.Spec.Quota.Hard = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("10"),
		}
		tmpl.Spec.NamespaceSelector = metav1.LabelSelector{
			MatchLabels: map[string]string{
				"team": "namespace1-team",
			},
		}
		Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

		// Successfully created singleton ResourceQuota
		rq := &corev1.ResourceQuota{}
		rq.Namespace = "namespace1"
		rq.Name = ResourceQuotaSingleton
		Eventually(komega.Get(rq)).Should(Succeed())

		// Delete ClusterResourceQuota
		Expect(k8sClient.Delete(ctx, tmpl)).To(Succeed())

		// No singleton ResourceQuota left, because no other ClusterResourceQuota exists.
		Eventually(komega.Get(rq)).ShouldNot(Succeed())
	})
})
