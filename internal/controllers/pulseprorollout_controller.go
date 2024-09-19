package controllers

import (
	"context"

	pulseprov1alpha1 "github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	"github.com/smarter-contracts/pulsepro-operator/internal/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PulseProRolloutReconciler reconciles a PulseProRollout object
type PulseProRolloutReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main Kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PulseProRolloutReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Fetch the PulseProRollout instance
	rollout := &pulseprov1alpha1.PulseProRollout{}
	if err := r.Get(ctx, req.NamespacedName, rollout); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return without requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object, requeue the request
		return ctrl.Result{}, err
	}

	// List all PulseProDeployment resources in the target namespace
	var pulseProDeployments pulseprov1alpha1.PulseProDeploymentList
	err := r.List(ctx, &pulseProDeployments, client.InNamespace(rollout.Spec.Namespace))
	if err != nil {
		l.Error(err, "Failed to list PulseProDeployments")
		return ctrl.Result{}, err
	}

	// Loop through the deployments and apply updates
	for _, deployment := range pulseProDeployments.Items {
		// Check if the deployment matches the rollout's tags and category using utility functions
		if utils.MatchesTags(deployment.Spec.Tags, rollout.Spec.Tags) && utils.MatchesCategory(deployment.Spec.Category, rollout.Spec.Category) {
			// Update the deployment with the new image version
			if deployment.Spec.PulseProVersion != rollout.Spec.ImageVersion {
				l.Info("Updating deployment", "deployment", deployment.Name, "namespace", deployment.Namespace, "newVersion", rollout.Spec.ImageVersion)
				deployment.Spec.PulseProVersion = rollout.Spec.ImageVersion
				if err := r.Update(ctx, &deployment); err != nil {
					l.Error(err, "Failed to update PulseProDeployment", "deployment", deployment.Name, "namespace", deployment.Namespace)
					continue
				}
				l.Info("Successfully updated deployment", "deployment", deployment.Name)
			} else {
				l.Info("Deployment already at target version", "deployment", deployment.Name, "version", rollout.Spec.ImageVersion)
			}
		}
	}

	// Update the status of the rollout (optional)
	rollout.Status.Phase = "Completed"
	if err := r.Status().Update(ctx, rollout); err != nil {
		l.Error(err, "Failed to update rollout status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PulseProRolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pulseprov1alpha1.PulseProRollout{}).
		Complete(r)
}
