package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	quotav1alpha1 "github.com/malolelandais/quota-operator/api/v1alpha1"
)

var _ = Describe("NamespaceQuotaPolicy Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		namespacequotapolicy := &quotav1alpha1.NamespaceQuotaPolicy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NamespaceQuotaPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, namespacequotapolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &quotav1alpha1.NamespaceQuotaPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: quotav1alpha1.NamespaceQuotaPolicySpec{
						TierAnnotation: "quota-operator/tier",
						Tiers:          quotav1alpha1.DefaultTiers(),
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &quotav1alpha1.NamespaceQuotaPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance NamespaceQuotaPolicy")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NamespaceQuotaPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should apply ResourceQuota when namespace is annotated", func() {
			By("Creating a namespace with tier annotation")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-quota-ns",
					Annotations: map[string]string{
						"quota-operator/tier": "small",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			By("Reconciling")
			controllerReconciler := &NamespaceQuotaPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the ResourceQuota was created")
			quota := &corev1.ResourceQuota{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "quota-operator-managed",
				Namespace: "test-quota-ns",
			}, quota)
			Expect(err).NotTo(HaveOccurred())
			Expect(quota.Spec.Hard[corev1.ResourcePods]).NotTo(BeNil())

			By("Cleanup namespace")
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})
	})
})
