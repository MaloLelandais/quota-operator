package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	quotav1alpha1 "github.com/malolelandais/quota-operator/api/v1alpha1"
)

var namespacequotapolicylog = logf.Log.WithName("namespacequotapolicy-resource")

func SetupNamespaceQuotaPolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &quotav1alpha1.NamespaceQuotaPolicy{}).
		WithValidator(&NamespaceQuotaPolicyCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-quota-malolelandais-dev-v1alpha1-namespacequotapolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=quota.malolelandais.dev,resources=namespacequotapolicies,verbs=create;update,versions=v1alpha1,name=vnamespacequotapolicy.kb.io,admissionReviewVersions=v1

type NamespaceQuotaPolicyCustomValidator struct{}

func (v *NamespaceQuotaPolicyCustomValidator) ValidateCreate(
	ctx context.Context, policy *quotav1alpha1.NamespaceQuotaPolicy,
) (admission.Warnings, error) {
	namespacequotapolicylog.Info("ValidateCreate", "name", policy.Name)
	return nil, validatePolicy(policy)
}

func (v *NamespaceQuotaPolicyCustomValidator) ValidateUpdate(
	ctx context.Context, oldObj, newObj *quotav1alpha1.NamespaceQuotaPolicy,
) (admission.Warnings, error) {
	namespacequotapolicylog.Info("ValidateUpdate", "name", newObj.Name)
	return nil, validatePolicy(newObj)
}

func (v *NamespaceQuotaPolicyCustomValidator) ValidateDelete(
	ctx context.Context, policy *quotav1alpha1.NamespaceQuotaPolicy,
) (admission.Warnings, error) {
	return nil, nil
}

func validatePolicy(policy *quotav1alpha1.NamespaceQuotaPolicy) error {
	if len(policy.Spec.Tiers) == 0 {
		return fmt.Errorf("spec.tiers must contain at least one tier")
	}
	for tierName, tierConfig := range policy.Spec.Tiers {
		if tierConfig.CPU.IsZero() {
			return fmt.Errorf("tier %q: cpu must be greater than 0", tierName)
		}
		if tierConfig.Memory.IsZero() {
			return fmt.Errorf("tier %q: memory must be greater than 0", tierName)
		}
		if tierConfig.MaxPods <= 0 {
			return fmt.Errorf("tier %q: maxPods must be greater than 0", tierName)
		}
	}
	return nil
}

// Nécessaire pour satisfaire l'interface runtime.Object
var _ runtime.Object = &quotav1alpha1.NamespaceQuotaPolicy{}
