package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QuotaTier définit les niveaux de quota disponibles
// +kubebuilder:validation:Enum=small;medium;large
type QuotaTier string

const (
	TierSmall  QuotaTier = "small"
	TierMedium QuotaTier = "medium"
	TierLarge  QuotaTier = "large"
)

// TierConfig définit les limites CPU et mémoire pour un tier
type TierConfig struct {
	// CPU total alloué au namespace (ex: "2")
	// +kubebuilder:validation:Required
	CPU resource.Quantity `json:"cpu"`

	// Mémoire totale allouée au namespace (ex: "4Gi")
	// +kubebuilder:validation:Required
	Memory resource.Quantity `json:"memory"`

	// Nombre maximum de pods dans le namespace
	// +kubebuilder:validation:Minimum=1
	MaxPods int32 `json:"maxPods"`
}

// NamespaceQuotaPolicySpec définit l'état désiré
type NamespaceQuotaPolicySpec struct {
	// Annotation K8s qui déclenche l'application du quota
	// ex: "quota-operator/tier"
	// +kubebuilder:default="quota-operator/tier"
	TierAnnotation string `json:"tierAnnotation,omitempty"`

	// Configuration pour chaque tier
	Tiers map[QuotaTier]TierConfig `json:"tiers"`
}

// NamespaceQuotaPolicyStatus définit l'état observé
type NamespaceQuotaPolicyStatus struct {
	// Nombre de namespaces gérés par cette policy
	ManagedNamespaces int32 `json:"managedNamespaces,omitempty"`

	// Conditions standards K8s (Ready, etc.)
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Dernière fois que la policy a été appliquée
	LastApplied *metav1.Time `json:"lastApplied,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Managed Namespaces",type=integer,JSONPath=`.status.managedNamespaces`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NamespaceQuotaPolicy est la CRD principale de notre opérateur
type NamespaceQuotaPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceQuotaPolicySpec   `json:"spec,omitempty"`
	Status NamespaceQuotaPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type NamespaceQuotaPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceQuotaPolicy `json:"items"`
}

// DefaultTiers retourne des valeurs par défaut raisonnables
func DefaultTiers() map[QuotaTier]TierConfig {
	return map[QuotaTier]TierConfig{
		TierSmall: {
			CPU:     resource.MustParse("1"),
			Memory:  resource.MustParse("2Gi"),
			MaxPods: 10,
		},
		TierMedium: {
			CPU:     resource.MustParse("4"),
			Memory:  resource.MustParse("8Gi"),
			MaxPods: 30,
		},
		TierLarge: {
			CPU:     resource.MustParse("8"),
			Memory:  resource.MustParse("16Gi"),
			MaxPods: 100,
		},
	}
}

func init() {
	SchemeBuilder.Register(&NamespaceQuotaPolicy{}, &NamespaceQuotaPolicyList{})
}
