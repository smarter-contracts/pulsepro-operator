package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PulseProRolloutSpec defines the desired state of PulseProRollout
type PulseProRolloutSpec struct {
	Namespace    string   `json:"namespace"`
	Tags         []string `json:"tags,omitempty"`
	Category     string   `json:"category,omitempty"`
	ImageVersion string   `json:"imageVersion"`
	Environments []string `json:"environments,omitempty"`
}

// PulseProRolloutStatus defines the observed state of PulseProRollout
type PulseProRolloutStatus struct {
	Phase string `json:"phase,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PulseProRollout is the Schema for the pulseprorollouts API
type PulseProRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PulseProRolloutSpec   `json:"spec,omitempty"`
	Status PulseProRolloutStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PulseProRolloutList contains a list of PulseProRollout
type PulseProRolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PulseProRollout `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PulseProRollout{}, &PulseProRolloutList{})
}
