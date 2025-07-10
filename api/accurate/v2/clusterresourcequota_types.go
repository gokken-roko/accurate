package v2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterResourceQuotaStatus defines the observed state of ClusterResourceQuota
type ClusterResourceQuotaStatus struct {
	// Hard is the set of enforced hard limits for each named resource
	// +optional
	Hard corev1.ResourceList `json:"hard,omitempty"`
	// Used is the current observed total usage of the resource in the namespace
	// and its descendant namespaces.
	// +optional
	Used corev1.ResourceList `json:"used,omitempty"`
}

// ClusterResourceQuotaSpec defines the desired state of ClusterResourceQuota
// example:
// apiVersion: accurate.cybozu.com/v2
// kind: ClusterResourceQuota
// metadata:
//
//	name: frontend-quota
//
// spec:
//
//	quota:
//	  hard:
//	    pods: "10"
//	    requests.storage: "100Mi"
//	namespaceSelector:
//	  annotations: null
//	  labels:
//	    matchLabels:
//	      team: frontend
type ClusterResourceQuotaSpec struct {
	// Quota is the quota to be enforced in namespaces
	// +optional
	Quota Quota `json:"quota,omitempty"`

	// NamespaceSelector is used to select namespaces that match the specified labels and annotations.
	// +optional
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// Quota defines the quota to be enforced in namespaces
type Quota struct {
	// Hard is the set of enforced hard limits for each named resource
	Hard corev1.ResourceList `json:"hard,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:storageversion
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+genclient

// ClusterResourceQuota is the Schema for the ClusterResourceQuota API
type ClusterResourceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the spec of ClusterResourceQuota
	// +optional
	Spec ClusterResourceQuotaSpec `json:"spec,omitempty"`

	// Status is the status of ClusterResourceQuota
	// +optional
	Status ClusterResourceQuotaStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterResourceQuotaList contains a list of ClusterResourceQuota
type ClusterResourceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceQuota{}, &ClusterResourceQuotaList{})
}
