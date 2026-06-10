package controller

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	quotav1alpha1 "github.com/malolelandais/quota-operator/api/v1alpha1"
)

var (
	// Nombre total de reconciliations
	reconciliationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quota_operator_reconciliations_total",
			Help: "Total number of reconciliations per policy",
		},
		[]string{"policy", "status"}, // labels: policy=default-policy, status=success|error
	)

	// Nombre de namespaces actuellement gérés
	managedNamespacesGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quota_operator_managed_namespaces",
			Help: "Number of namespaces currently managed by a policy",
		},
		[]string{"policy"},
	)

	// Durée des reconciliations
	reconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "quota_operator_reconciliation_duration_seconds",
			Help:    "Duration of reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"policy"},
	)
)

func init() {
	// Enregistre les métriques auprès du registry controller-runtime
	metrics.Registry.MustRegister(
		reconciliationsTotal,
		managedNamespacesGauge,
		reconciliationDuration,
	)
}

type NamespaceQuotaPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=quota.malolelandais.dev,resources=namespacequotapolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=quota.malolelandais.dev,resources=namespacequotapolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete

func (r *NamespaceQuotaPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	start := time.Now() // ← démarre le timer

	logger.Info("Reconciling NamespaceQuotaPolicy", "name", req.Name)

	// 1. Récupère la NamespaceQuotaPolicy
	policy := &quotav1alpha1.NamespaceQuotaPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		reconciliationsTotal.WithLabelValues(req.Name, "error").Inc()
		return ctrl.Result{}, fmt.Errorf("failed to get NamespaceQuotaPolicy: %w", err)
	}

	// 2. Liste tous les namespaces du cluster
	namespaceList := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaceList); err != nil {
		reconciliationsTotal.WithLabelValues(req.Name, "error").Inc()
		return ctrl.Result{}, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// 3. Pour chaque namespace, vérifie s'il a l'annotation quota-tier
	managedCount := int32(0)
	annotation := policy.Spec.TierAnnotation
	if annotation == "" {
		annotation = "quota-operator/tier"
	}

	for _, ns := range namespaceList.Items {
		tierValue, ok := ns.Annotations[annotation]
		if !ok {
			continue
		}

		tier := quotav1alpha1.QuotaTier(tierValue)
		tierConfig, exists := policy.Spec.Tiers[tier]
		if !exists {
			logger.Info("Unknown tier, skipping", "namespace", ns.Name, "tier", tierValue)
			continue
		}

		if err := r.applyResourceQuota(ctx, ns.Name, tier, tierConfig); err != nil {
			logger.Error(err, "Failed to apply ResourceQuota", "namespace", ns.Name)
			continue
		}

		logger.Info("Applied quota", "namespace", ns.Name, "tier", tierValue)
		managedCount++
	}

	// 4. Met à jour le status
	policy.Status.ManagedNamespaces = managedCount
	now := metav1.Now()
	policy.Status.LastApplied = &now
	if err := r.Status().Update(ctx, policy); err != nil {
		reconciliationsTotal.WithLabelValues(req.Name, "error").Inc()
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// 5. Enregistre les métriques
	reconciliationsTotal.WithLabelValues(req.Name, "success").Inc()
	managedNamespacesGauge.WithLabelValues(req.Name).Set(float64(managedCount))
	reconciliationDuration.WithLabelValues(req.Name).Observe(time.Since(start).Seconds())

	return ctrl.Result{}, nil
}

// applyResourceQuota crée ou met à jour le ResourceQuota d'un namespace
func (r *NamespaceQuotaPolicyReconciler) applyResourceQuota(
	ctx context.Context,
	namespace string,
	tier quotav1alpha1.QuotaTier,
	config quotav1alpha1.TierConfig,
) error {
	quotaName := "quota-operator-managed"

	// Construit le ResourceQuota désiré
	desired := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      quotaName,
			Namespace: namespace,
			Labels: map[string]string{
				"managed-by": "quota-operator",
				"tier":       string(tier),
			},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU:    config.CPU,
				corev1.ResourceMemory: config.Memory,
				corev1.ResourcePods:   *resource.NewMilliQuantity(int64(config.MaxPods)*1000, resource.DecimalSI),
			},
		},
	}

	// Vérifie si le ResourceQuota existe déjà
	existing := &corev1.ResourceQuota{}
	err := r.Get(ctx, types.NamespacedName{Name: quotaName, Namespace: namespace}, existing)

	if errors.IsNotFound(err) {
		// Crée le ResourceQuota
		return r.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("failed to get ResourceQuota: %w", err)
	}

	// Met à jour si les specs ont changé
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func (r *NamespaceQuotaPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.NamespaceQuotaPolicy{}).
		// Surveille aussi les Namespaces pour réagir quand une annotation change
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.namespaceToPolicy),
		).
		Complete(r)
}

// namespaceToPolicy mappe un événement Namespace vers la NamespaceQuotaPolicy concernée
func (r *NamespaceQuotaPolicyReconciler) namespaceToPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	// Liste toutes les policies et les enqueue pour reconciliation
	policyList := &quotav1alpha1.NamespaceQuotaPolicyList{}
	if err := r.List(ctx, policyList); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(policyList.Items))
	for i, policy := range policyList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: policy.Name,
			},
		}
	}
	return requests
}
