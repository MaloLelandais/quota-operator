package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	quotav1alpha1 "github.com/malolelandais/quota-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func setupClient(t *testing.T) (client.Client, *kubernetes.Clientset) {
	t.Helper()

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to build kubeconfig: %v", err)
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = quotav1alpha1.AddToScheme(scheme)

	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}

	return c, cs
}

func TestOperatorE2E(t *testing.T) {
	ctx := context.Background()
	c, cs := setupClient(t)

	// 1. Crée la NamespaceQuotaPolicy
	t.Log("Creating NamespaceQuotaPolicy...")
	policy := &quotav1alpha1.NamespaceQuotaPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-policy",
		},
		Spec: quotav1alpha1.NamespaceQuotaPolicySpec{
			TierAnnotation: "quota-operator/tier",
			Tiers: map[quotav1alpha1.QuotaTier]quotav1alpha1.TierConfig{
				quotav1alpha1.TierSmall: {
					CPU:     resource.MustParse("500m"),
					Memory:  resource.MustParse("512Mi"),
					MaxPods: 5,
				},
			},
		},
	}
	if err := c.Create(ctx, policy); err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}
	defer func() { _ = c.Delete(ctx, policy) }()

	// 2. Crée un namespace annoté
	t.Log("Creating annotated namespace...")
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-test-ns",
			Annotations: map[string]string{
				"quota-operator/tier": "small",
			},
		},
	}
	if _, err := cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = cs.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{}) }()

	// 3. Attend que le ResourceQuota soit créé (max 60s)
	t.Log("Waiting for ResourceQuota to be created...")
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		quota := &corev1.ResourceQuota{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      "quota-operator-managed",
			Namespace: "e2e-test-ns",
		}, quota)
		if err != nil {
			return false, nil
		}
		cpu := quota.Spec.Hard[corev1.ResourceCPU]
		memory := quota.Spec.Hard[corev1.ResourceMemory]
		pods := quota.Spec.Hard[corev1.ResourcePods]
		t.Logf("ResourceQuota found: CPU=%s Memory=%s Pods=%s",
			cpu.String(),
			memory.String(),
			pods.String(),
		)
		return true, nil
	})
	if err != nil {
		t.Fatalf("ResourceQuota was not created within timeout: %v", err)
	}

	// 4. Vérifie les valeurs du quota
	t.Log("Verifying ResourceQuota values...")
	quota := &corev1.ResourceQuota{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      "quota-operator-managed",
		Namespace: "e2e-test-ns",
	}, quota); err != nil {
		t.Fatalf("Failed to get ResourceQuota: %v", err)
	}

	cpu := quota.Spec.Hard[corev1.ResourceCPU]
	if cpu.Cmp(resource.MustParse("500m")) != 0 {
		t.Errorf("Expected CPU 500m, got %s", cpu.String())
	}

	memory := quota.Spec.Hard[corev1.ResourceMemory]
	if memory.Cmp(resource.MustParse("512Mi")) != 0 {
		t.Errorf("Expected Memory 512Mi, got %s", memory.String())
	}

	// 5. Vérifie que le status de la policy est mis à jour
	t.Log("Verifying policy status...")
	updatedPolicy := &quotav1alpha1.NamespaceQuotaPolicy{}
	if err := c.Get(ctx, types.NamespacedName{Name: "e2e-policy"}, updatedPolicy); err != nil {
		t.Fatalf("Failed to get policy: %v", err)
	}
	if updatedPolicy.Status.ManagedNamespaces == 0 {
		t.Error("Expected ManagedNamespaces > 0")
	}

	t.Logf("E2E test passed — ManagedNamespaces: %d", updatedPolicy.Status.ManagedNamespaces)
}
