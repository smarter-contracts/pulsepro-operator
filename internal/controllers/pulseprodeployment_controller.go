package controllers

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	pulseprov1alpha1 "github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PulseProDeploymentReconciler is the reconciler for PulseProDeployment CRD
type PulseProDeploymentReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme // +kubebuilder:scaffold:scheme
}

// +kubebuilder:rbac:groups=pulsepro.io,resources=pulseprodeployments,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *PulseProDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PulseProDeployment{}). // Watch for changes to PulseProDeployment
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

	// Check connectivity to external services (Vault, MidTier, RabbitMQ, Postgres, TimescaleDB)
	if err := checkConnectivity(); err != nil {
		log.Error(err, "Failed to connect to external services")
		instance.Status.Status = "Failed"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, nil
	}

	// Run Helm Install/Upgrade
	if err := runHelmRelease(instance.Spec, helmValues); err != nil {
		log.Error(err, "Helm release failed")
		instance.Status.Status = "Failed"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, nil
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

// checkConnectivity pings external services (Vault, MidTier, RabbitMQ, Postgres, TimescaleDB)
// to ensure they are reachable before applying any changes.
func checkConnectivity() error {
	services := []string{"Vault", "MidTier", "RabbitMQ", "Postgres", "TimescaleDB"}

	for _, service := range services {
		cmd := exec.Command("ping", "-c", "1", service)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to connect to %s", service)
		}
	}
	return nil
}

// runHelmRelease executes the Helm install/upgrade command with the specified Helm values.
func runHelmRelease(spec pulseprov1alpha1.PulseProDeploymentSpec, values string) error {
	helmCmd := exec.Command("helm", "upgrade", "--install", spec.PulseProVersion, "--values", values)
	return helmCmd.Run()
}
