package controllers

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	pulseprov1alpha1 "github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	"github.com/smarter-contracts/pulsepro-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PulseProDeploymentReconciler is the reconciler for PulseProDeployment CRD
type PulseProDeploymentReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	KubeContext string
}

// PulseProValues holds the configuration for external services
type PulseProValues struct {
	Midtier struct {
		Host string `yaml:"host"`
	} `yaml:"midtier"`
	Vault struct {
		Address string `yaml:"address"`
	} `yaml:"vault"`
	RabbitMQ struct {
		Host string `yaml:"host"`
	} `yaml:"rabbitmq"`
	TimescaleDB struct {
		Host string `yaml:"timescaledb"`
	} `yaml:"timescaledb"`
	Postgres struct {
		Host string `yaml:"postgres"`
	} `yaml:"postgres"`
}

// RolloutConfig represents the configuration for rolling out updates to PulsePro deployments
type RolloutConfig struct {
	Rollouts []Rollout `yaml:"rollouts"`
}

// Rollout represents a specific rollout, including the environments, tags, category, and image version
type Rollout struct {
	Namespace    string   `yaml:"namespace"`
	Environments []string `yaml:"environments"`
	Tags         []string `yaml:"tags"`
	Category     string   `yaml:"category"`
	ImageVersion string   `yaml:"imageVersion"`
}

// +kubebuilder:rbac:groups=pulsepro.io,resources=pulseprodeployments,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *PulseProDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PulseProDeployment{}).
		Complete(r)
}

// Reconcile is the core function that checks the CRD and applies the desired state
func (r *PulseProDeploymentReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("pulseprodeployment", req.NamespacedName)

	// Fetch the PulseProDeployment instance
	instance := &pulseprov1alpha1.PulseProDeployment{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return without requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object, requeue the request
		return reconcile.Result{}, err
	}

	// Fetch ConfigMap for Helm values
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.HelmValuesConfigMap.Name, Namespace: req.Namespace}, cm); err != nil {
		log.Error(err, "Unable to fetch ConfigMap")
		instance.Status.Status = "Failed to fetch ConfigMap"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, err
	}

	helmValues := cm.Data[instance.Spec.HelmValuesConfigMap.Key]

	// Load PulseProValues from ConfigMap data
	values, err := loadConfig(helmValues)
	if err != nil {
		log.Error(err, "Failed to load PulsePro values from ConfigMap")
		return reconcile.Result{}, err
	}

	// GitOps Sync: pull latest changes from GitHub repository
	if err := r.syncFromGitRepo(instance.Spec.GitRepoURL, "/tmp/repo"); err != nil {
		log.Error(err, "GitOps sync failed")
		return reconcile.Result{}, err
	}

	// Define paths based on project and environment
	projectName := instance.Spec.ProjectName
	environmentName := instance.Spec.EnvironmentName

	// Paths to the secrets and values files
	secretsDir := fmt.Sprintf("/tmp/repo/environments/%s-%s/secrets/pulse-pro", projectName, environmentName)
	secretsFile := fmt.Sprintf("%s/secrets.yaml", secretsDir)
	secretsEncFile := secretsFile + ".dec"

	// Define the helmfile type from the spec, with a default of "gke"
	helmfileType := instance.Spec.HelmfileType
	if helmfileType == "" {
		helmfileType = "gke"
	}

	helmfilePath := fmt.Sprintf("/tmp/repo/helmfiles/pulse-pro/%s/helmfile.yaml", helmfileType)

	// Define the core values file path
	// coreValuesFilePath := fmt.Sprintf("/tmp/repo/environments/%s-%s/values/pulse-pro/values.yaml.gotmpl", projectName, environmentName)

	// Check if the encrypted secrets file (.yaml.dec) exists
	if _, err := os.Stat(secretsEncFile); os.IsNotExist(err) {
		log.Error(err, "Encrypted secrets file does not exist", "file", secretsEncFile)
		instance.Status.Status = "Encrypted secrets file missing"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, nil
	}

	// Decrypt the secrets using helmfile's `secrets` integration
	// if err := decryptSecrets(secretsEncFile, secretsFile); err != nil {
	// 	log.Error(err, "Failed to decrypt secrets")
	// 	instance.Status.Status = "Failed to decrypt secrets"
	// 	_ = r.Status().Update(ctx, instance)
	// 	return reconcile.Result{}, err
	// }

	// Check connectivity to external services (Vault, MidTier, RabbitMQ, Postgres, TimescaleDB)
	if err := checkConnectivity(values); err != nil {
		log.Error(err, "Failed to connect to external services")
		instance.Status.Status = "Failed"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, nil
	}

	// Use helmfile to apply Helm changes
	if err := runHelmfileSync(helmfilePath, projectName, environmentName, r.KubeContext); err != nil {
		log.Error(err, "Helmfile sync failed")
		instance.Status.Status = "Helmfile sync failed"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, err
	}

	// Update the status of the PulseProDeployment to "Synced"
	instance.Status.Status = "Synced"
	if err := r.Status().Update(ctx, instance); err != nil {
		return reconcile.Result{}, err
	}

	// Requeue the request after the sync interval for periodic reconciliation
	syncInterval, err := time.ParseDuration(instance.Spec.SyncInterval)
	if err != nil {
		// Default requeue time if parsing fails
		syncInterval = 10 * time.Minute
	}
	return reconcile.Result{RequeueAfter: syncInterval}, nil
}

func encryptSecrets(plainFile, encFile string) error {
	// Prepare the helm secrets encrypt command
	cmd := exec.Command("helm", "secrets", "encrypt", plainFile)

	// Create buffers to capture stdout and stderr
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Run the encryption command
	if err := cmd.Run(); err != nil {
		// Capture the error and stderr output
		return fmt.Errorf("failed to encrypt secrets: %v\nStderr: %s", err, errBuf.String())
	}

	// Write the encrypted output to the target file
	if err := os.WriteFile(encFile, outBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write encrypted secrets to file: %v", err)
	}

	return nil
}

// decryptSecrets decrypts the encrypted secrets file (.yaml.dec) into the target file (.yaml)
// using the helm secrets plugin which internally uses sops.
func decryptSecrets(encFile, outputFile string) error {
	// Prepare the command to decrypt the secrets using helm secrets
	cmd := exec.Command("helm", "secrets", "decrypt", encFile)

	// Create buffers to capture stdout and stderr
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Run the decryption command
	if err := cmd.Run(); err != nil {
		// Capture the error and stderr output
		return fmt.Errorf("failed to decrypt secrets using helm secrets: %v\nStderr: %s", err, errBuf.String())
	}

	// Write the decrypted output to the specified file
	if err := os.WriteFile(outputFile, outBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write decrypted secrets to file: %v", err)
	}

	return nil
}

// Function to load ConfigMap data into PulseProValues
func loadConfig(data string) (*PulseProValues, error) {
	var values PulseProValues
	err := yaml.Unmarshal([]byte(data), &values)
	if err != nil {
		return nil, fmt.Errorf("failed to parse values from ConfigMap: %v", err)
	}
	return &values, nil
}

// sanitizeHost strips the protocol (if present) from the host URL
func sanitizeHost(rawHost string) (string, error) {
	// If the host includes a protocol (http/https), remove it
	if strings.HasPrefix(rawHost, "http://") || strings.HasPrefix(rawHost, "https://") {
		u, err := url.Parse(rawHost)
		if err != nil {
			return "", err
		}
		return u.Hostname(), nil
	}
	return rawHost, nil
}

// checkConnectivity checks the connectivity for the external services
func checkConnectivity(values *PulseProValues) error {
	// Services to check for connectivity
	services := map[string]string{
		"Vault":       values.Vault.Address,
		"MidTier":     values.Midtier.Host,
		"RabbitMQ":    values.RabbitMQ.Host,
		"TimescaleDB": values.TimescaleDB.Host,
		"Postgres":    values.Postgres.Host,
	}

	// Iterate through the services and check connectivity
	for service, host := range services {
		if host == "" {
			// Skip connectivity check if host is empty
			fmt.Printf("Skipping connectivity check for %s: no hostname provided in the ConfigMap\n", service)
			continue
		}

		// Skip internal services check in local environments
		if service == "RabbitMQ" || service == "TimescaleDB" || service == "Postgres" || service == "MidTier" {
			fmt.Printf("Skipping connectivity check for %s in local environment\n", service)
			continue
		}

		// Check HTTP(S) services like Vault and MidTier using curl with -L to follow redirects
		if service == "Vault" || service == "MidTier" {
			fmt.Printf("Checking HTTP connectivity for %s at %s\n", service, host)
			cmd := exec.Command("curl", "-L", "-s", "-o", "/dev/null", "-w", "%{http_code}", host)
			output, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("failed to connect to %s (%s): %v", service, host, err)
			}

			// Check if the HTTP status code is 200
			if string(output) != "200" {
				return fmt.Errorf("failed to connect to %s (%s): received HTTP status %s", service, host, string(output))
			}

			fmt.Printf("Successfully connected to %s (%s) with HTTP status 200\n", service, host)
			continue
		}

		// For other services, use ping to test connectivity
		fmt.Printf("Checking connectivity for %s at %s\n", service, host)
		cmd := exec.Command("ping", "-c", "1", host)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to connect to %s (%s): %v", service, host, err)
		}

		// Log successful connection
		fmt.Printf("Successfully connected to %s (%s)\n", service, host)
	}

	return nil
}

func isRunningInCluster() bool {
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount")
	return err == nil
}

// runHelmfileSync runs the helmfile sync command with the specified parameters
func runHelmfileSync(helmfilePath, projectName, environmentName, kubeContext string) error {
	// Construct the helmfile sync command
	cmdArgs := []string{"-f", helmfilePath, "--environment", projectName + "-" + environmentName, "sync"}
	if kubeContext != "" {
		cmdArgs = append(cmdArgs, "--kube-context", kubeContext)
	}

	// Log the Helmfile command being executed
	fmt.Printf("DEBUG: Executing Helmfile command: helmfile %s\n", strings.Join(cmdArgs, " "))

	// Path to the secrets file
	secretsFilePath := fmt.Sprintf("/tmp/repo/environments/%s-%s/secrets/pulse-pro/secrets.yaml", projectName, environmentName)

	// Check if the secrets file is encrypted or not
	if !isFileEncrypted(secretsFilePath) {
		// Skip decryption and log the action
		fmt.Printf("DEBUG: Secrets file %s is not encrypted, treating as a regular values file.\n", secretsFilePath)
	}

	// Create the helmfile command
	helmfileCmd := exec.Command("helmfile", cmdArgs...)

	// Capture the combined output (stdout and stderr)
	output, err := helmfileCmd.CombinedOutput()

	if err != nil {
		// Log the command output and the error
		fmt.Printf("Helmfile sync failed: %v\nOutput: %s\n", err, string(output))
		return fmt.Errorf("helmfile sync failed: %v\nOutput: %s", err, string(output))
	}

	// Log successful sync output
	fmt.Printf("Helmfile sync succeeded. Output: %s\n", string(output))
	return nil
}

// isFileEncrypted checks if a file is encrypted by analyzing its contents or file extension.
func isFileEncrypted(filePath string) bool {
	// In this case, assume that any file ending with ".yaml.dec" is decrypted and ".yaml" is encrypted
	return strings.HasSuffix(filePath, ".yaml.dec")
}

// runHelmRelease executes the Helm upgrade/install command with the provided values file
func runHelmRelease(spec pulseprov1alpha1.PulseProDeploymentSpec, valuesFilePath string, secretsFilePath string, coreValuesFilePath string, kubeContext string) error {
	// Define the release name
	releaseName := spec.ProjectName + "-" + spec.EnvironmentName

	// Use the Helm chart and version from the spec
	chartRepo := spec.HelmChart
	chartVersion := spec.HelmChartVersion

	// Authenticate with GCP and log into the Artifact Registry (GCR)
	accessTokenCmd := exec.Command("gcloud", "auth", "print-access-token")
	accessToken, err := accessTokenCmd.Output()
	if err != nil {
		fmt.Printf("Failed to get access token from gcloud: %v\n", err)
		return err
	}

	// Use the access token to log into the Artifact Registry
	helmLoginCmd := exec.Command("helm", "registry", "login", "-u", "oauth2accesstoken", "--password-stdin", "europe-docker.pkg.dev")
	helmLoginCmd.Stdin = strings.NewReader(string(accessToken))
	helmLoginOutput, err := helmLoginCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Helm registry login failed: %v\nOutput: %s\n", err, string(helmLoginOutput))
		return err
	}

	fmt.Println("Successfully logged into Helm registry")

	// Ensure the values file exists
	if _, err := os.Stat(valuesFilePath); os.IsNotExist(err) {
		fmt.Printf("Values file does not exist: %s\n", valuesFilePath)
		return err
	}

	// Ensure the secrets file exists
	if _, err := os.Stat(secretsFilePath); os.IsNotExist(err) {
		fmt.Printf("Secrets file does not exist: %s\n", secretsFilePath)
		return err
	}

	// Ensure the core values file exists
	if _, err := os.Stat(coreValuesFilePath); os.IsNotExist(err) {
		fmt.Printf("Core values file does not exist: %s\n", coreValuesFilePath)
		return err
	}

	// Determine if the operator is running inside a Kubernetes cluster
	var helmCmd *exec.Cmd
	if isRunningInCluster() || kubeContext == "" {
		// Use in-cluster configuration, no need for kube-context
		helmCmd = exec.Command("helm", "upgrade", "--install", releaseName, chartRepo, "--version", chartVersion,
			"--values", valuesFilePath,
			"--values", secretsFilePath,
			"--values", coreValuesFilePath)
	} else {
		// Use the provided kube-context for local development
		helmCmd = exec.Command("helm", "upgrade", "--install", releaseName, chartRepo, "--version", chartVersion,
			"--values", valuesFilePath,
			"--values", secretsFilePath,
			"--values", coreValuesFilePath,
			"--kube-context", kubeContext)
	}

	// Log the Helm command before executing
	fmt.Printf("Executing Helm command: %s %s\n", helmCmd.Path, strings.Join(helmCmd.Args, " "))

	// Capture the combined output (stdout and stderr)
	output, err := helmCmd.CombinedOutput()

	if err != nil {
		// Log the command output and the error
		fmt.Printf("Helm release failed: %v\nOutput: %s\n", err, string(output))
		return err
	}

	fmt.Printf("Helm release succeeded. Output: %s\n", string(output))
	return nil
}

// syncFromGitRepo clones or pulls the latest changes from the Git repository
func (r *PulseProDeploymentReconciler) syncFromGitRepo(repoURL, repoDir string) error {
	// Check if the repo already exists
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// Clone the repository if not present
		r.Log.Info("Cloning repository", "repoURL", repoURL)
		_, err := git.PlainClone(repoDir, false, &git.CloneOptions{
			URL: repoURL,
		})
		if err != nil {
			return fmt.Errorf("failed to clone repository: %v", err)
		}
	} else {
		// Check if the .git directory exists inside repoDir
		if _, err := os.Stat(fmt.Sprintf("%s/.git", repoDir)); os.IsNotExist(err) {
			return fmt.Errorf("repository exists but does not have a .git directory")
		}

		// Open the existing repository
		r.Log.Info("Pulling latest changes from repository", "repoURL", repoURL)
		repo, err := git.PlainOpen(repoDir)
		if err != nil {
			r.Log.Error(err, "Repository does not exist or is not initialized correctly", "repoDir", repoDir)
			return fmt.Errorf("failed to open repository: %v", err)
		}

		// Log the current branch or commit
		head, err := repo.Head()
		if err == nil {
			r.Log.Info("Repository opened", "branch", head.Name().String(), "commit", head.Hash().String())
		} else {
			r.Log.Error(err, "Failed to get repository head")
		}

		// Get the worktree and pull the latest changes
		w, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("failed to get worktree: %v", err)
		}

		// Pull with additional error handling
		err = w.Pull(&git.PullOptions{RemoteName: "origin"})
		if err == git.NoErrAlreadyUpToDate {
			r.Log.Info("Repository is already up to date", "repoDir", repoDir)
		} else if err != nil {
			r.Log.Error(err, "Failed to pull latest changes", "repoDir", repoDir)
			return fmt.Errorf("failed to pull repository: %v", err)
		}
	}

	return nil
}

// updateConfigMap updates the ConfigMap with the latest values from the Git repository
func (r *PulseProDeploymentReconciler) updateConfigMap(repoDir, valuesFile, valuesSubDir, secretsFile, namespace string) error {
	// Initialize a map to hold all combined values
	combinedData := make(map[string]string)

	// Read the base values file
	data, err := os.ReadFile(valuesFile)
	if err != nil {
		return fmt.Errorf("failed to read base values file: %v", err)
	}
	combinedData["values.yaml"] = string(data)

	// Read the values from the subdirectory (values.yaml.gotmpl)
	subDirData, err := os.ReadFile(valuesSubDir)
	if err != nil {
		return fmt.Errorf("failed to read subdirectory values file: %v", err)
	}
	// Change the key to use '.' instead of '/'
	combinedData["values.values.yaml.gotmpl"] = string(subDirData)

	// Get the existing ConfigMap
	configMap := &corev1.ConfigMap{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: "pulsepro-helm-values", Namespace: namespace}, configMap)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap: %v", err)
	}

	// Update the ConfigMap's values with the combined data
	for key, value := range combinedData {
		configMap.Data[key] = value
	}

	// Update the ConfigMap with the new data
	err = r.Update(context.TODO(), configMap)
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %v", err)
	}

	r.Log.Info("ConfigMap updated successfully with values and secrets")
	return nil
}

// UpdatePulseProDeployments updates PulsePro deployments based on tags and category
func UpdatePulseProDeployments(k8sClient client.Client, config *RolloutConfig) error {
	ctx := context.TODO()

	for _, rollout := range config.Rollouts {
		for _, deploymentName := range rollout.Environments {
			var pulseProDeployment pulseprov1alpha1.PulseProDeployment
			err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: rollout.Namespace,
				Name:      deploymentName,
			}, &pulseProDeployment)

			if err != nil {
				fmt.Printf("Error fetching PulseProDeployment %s in namespace %s: %v\n", deploymentName, rollout.Namespace, err)
				continue
			}

			// Check if tags or category match the rollout criteria
			if !utils.MatchesTags(pulseProDeployment.Spec.Tags, rollout.Tags) || !utils.MatchesCategory(pulseProDeployment.Spec.Category, rollout.Category) {
				fmt.Printf("Skipping deployment %s in namespace %s: tags or category do not match\n", deploymentName, rollout.Namespace)
				continue
			}

			if pulseProDeployment.Spec.PulseProVersion != rollout.ImageVersion {
				fmt.Printf("Updating %s in namespace %s to image version %s\n", deploymentName, rollout.Namespace, rollout.ImageVersion)

				pulseProDeployment.Spec.PulseProVersion = rollout.ImageVersion
				err = k8sClient.Update(ctx, &pulseProDeployment)

				if err != nil {
					fmt.Printf("Error updating PulseProDeployment %s: %v\n", deploymentName, err)
				} else {
					fmt.Printf("Successfully updated %s in namespace %s to image version %s\n", deploymentName, rollout.Namespace, rollout.ImageVersion)
				}
			} else {
				fmt.Printf("%s is already at version %s\n", deploymentName, rollout.ImageVersion)
			}
		}
	}

	return nil
}
