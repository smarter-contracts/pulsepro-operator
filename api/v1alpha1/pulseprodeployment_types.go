package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PulseProDeploymentSpec defines the desired state of PulseProDeployment
type PulseProDeploymentSpec struct {
	// Namespace is the Kubernetes namespace where PulsePro will be deployed
	Namespace string `json:"namespace"`

	// GitRepoURL is the URL of the Git repository used for GitOps sync
	GitRepoURL string `json:"gitRepoURL,omitempty"`

	// HelmChart is the Helm chart to be used for deployment
	HelmChart string `json:"helmChart"`

	// HelmChartVersion is the version of the Helm chart to be used for deployment
	HelmChartVersion string `json:"helmChartVersion"`

	// HelmfileType is the type of Helmfile to be used for deployment
	HelmfileType string `json:"helmfileType,omitempty"`

	// PulseProVersion is the specific version of PulsePro to be deployed
	PulseProVersion string `json:"pulseProVersion"`

	// HelmValuesConfigMap is a reference to the ConfigMap containing Helm chart values
	HelmValuesConfigMap ConfigMapReference `json:"helmValuesConfigMap"`

	// Secrets contains a list of Kubernetes secrets required for the deployment
	Secrets []SecretReference `json:"secrets"`

	// ProjectName defines the name of the project
	ProjectName string `json:"projectName"`

	// EnvironmentName defines the environment (e.g., staging, production)
	EnvironmentName string `json:"environmentName"`

	// SyncInterval defines the time interval for syncing GitOps changes
	SyncInterval string `json:"syncInterval"`

	// Tags define labels that categorise the PulsePro deployment (e.g., "company_name", "test", "EU", "critical")
	Tags []string `json:"tags,omitempty"`

	// Category groups deployments into categories (e.g., "production", "staging", "sandbox")
	Category string `json:"category,omitempty"`
}

// ConfigMapReference defines a reference to a ConfigMap
type ConfigMapReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// SecretReference defines a reference to a Kubernetes Secret
type SecretReference struct {
	Name      string `json:"name"`
	ValueFrom string `json:"valueFrom"`
}

// PulseProDeploymentStatus defines the observed state of PulseProDeployment
type PulseProDeploymentStatus struct {
	// Status shows the current status of the deployment (e.g., Synced, Failed, etc.)
	Status string `json:"status,omitempty"`

	// CurrentVersion is the current version of PulsePro being deployed
	CurrentVersion string `json:"currentVersion,omitempty"`

	// LastAppliedConfigMap indicates the last applied ConfigMap for Helm values
	LastAppliedConfigMap string `json:"lastAppliedConfigMap,omitempty"`

	// LastSuccessfulReconcile shows the timestamp of the last successful reconciliation
	LastSuccessfulReconcile string `json:"lastSuccessfulReconcile,omitempty"`

	// PreviousVersion holds the version of PulsePro before the current deployment
	PreviousVersion string `json:"previousVersion,omitempty"`

	// PreviousConfigMap shows the ConfigMap that was used in the previous deployment
	PreviousConfigMap string `json:"previousConfigMap,omitempty"`

	// RollbackInProgress is true when a rollback is happening
	RollbackInProgress bool `json:"rollbackInProgress,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PulseProDeployment is the Schema for the pulseprodeployments API
type PulseProDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PulseProDeploymentSpec   `json:"spec,omitempty"`
	Status PulseProDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PulseProDeploymentList contains a list of PulseProDeployment resources
type PulseProDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PulseProDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PulseProDeployment{}, &PulseProDeploymentList{})
}
