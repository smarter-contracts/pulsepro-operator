package controllers

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pulseprov1alpha1 "github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type PulseProDeploymentReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=pulsepro.io,resources=pulseprodeployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the core function that checks the CRD and applies the desired state
func (r *PulseProDeploymentReconciler) Reconcile(req reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("pulseprodeployment", req.NamespacedName)

	// Fetch the PulseProDeployment instance
	instance := &pulseprov1alpha1.PulseProDeployment{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch ConfigMap for Helm values
	cm := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Spec.HelmValuesConfigMap.Name, Namespace: req.Namespace}, cm)
	if err != nil {
		log.Error(err, "Unable to fetch ConfigMap")
		instance.Status.Status = "Failed to fetch ConfigMap"
		_ = r.Status().Update(ctx, instance)
		return reconcile.Result{}, err
	}

	helmValues := cm.Data[instance.Spec.HelmValuesConfigMap.Key]

	// Check connectivity to external services
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

	// Update the status
	instance.Status.Status = "Synced"
	err = r.Status().Update(ctx, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Requeue after sync interval for reconciliation
	syncInterval, _ := time.ParseDuration(instance.Spec.SyncInterval)
	return reconcile.Result{RequeueAfter: syncInterval}, nil
}

// Check connectivity to Vault, MidTier, RabbitMQ, Postgres, TimescaleDB
func checkConnectivity() error {
	services := []string{"Vault", "MidTier", "RabbitMQ", "Postgres", "TimescaleDB"}

	for _, service := range services {
		cmd := exec.Command("ping", "-c", "1", service)
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to connect to %s", service)
		}
	}
	return nil
}

// runHelmRelease runs the Helm install/upgrade command
func runHelmRelease(spec pulseprov1alpha1.PulseProDeploymentSpec, values string) error {
	helmCmd := exec.Command("helm", "upgrade", "--install", spec.PulseProVersion, "--values", values)
	return helmCmd.Run()
}
