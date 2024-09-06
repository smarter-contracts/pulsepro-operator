package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PulseProDeploymentSpec struct {
	HelmChartVersion    string             `json:"helmChartVersion"`
	PulseProVersion     string             `json:"pulseProVersion"`
	HelmValuesConfigMap ConfigMapReference `json:"helmValuesConfigMap"`
	Secrets             []SecretReference  `json:"secrets"`
	Namespace           string             `json:"namespace"`
	SyncInterval        string             `json:"syncInterval"`
}

type ConfigMapReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type SecretReference struct {
	Name      string `json:"name"`
	ValueFrom string `json:"valueFrom"`
}

type PulseProDeploymentStatus struct {
	Status string `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type PulseProDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PulseProDeploymentSpec   `json:"spec,omitempty"`
	Status PulseProDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type PulseProDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PulseProDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PulseProDeployment{}, &PulseProDeploymentList{})
}
