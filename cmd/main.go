package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/exec"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	pulseprov1alpha1 "github.com/smarter-contracts/pulsepro-operator/api/v1alpha1"
	"github.com/smarter-contracts/pulsepro-operator/internal/controllers"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(pulseprov1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		secureMetrics        bool
		enableHTTP2          bool
		enableWebhooks       bool
		kubeContext          string // Add kubeContext flag for local development
		tlsOpts              []func(*tls.Config)
	)

	// Define the kube-context flag and other CLI flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&kubeContext, "kube-context", "", "The Kubernetes context to use for local development (leave empty for in-cluster config)")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "Serve the metrics endpoint securely via HTTPS.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2 for the metrics and webhook servers.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", true, "Enable webhooks for the operator.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Disable HTTP/2 if the flag is set to false
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("HTTP/2 is disabled for security reasons", "reason", "HTTP/2 vulnerabilities")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "cf2fb68a.pulsepro.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Register the PulseProDeploymentReconciler with the manager and pass kubeContext
	if err := (&controllers.PulseProDeploymentReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("PulseProDeployment"),
		Scheme:      mgr.GetScheme(),
		KubeContext: kubeContext,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PulseProDeployment")
		os.Exit(1)
	}

	// Register webhook if enabled
	if enableWebhooks {
		if err = (&pulseprov1alpha1.PulseProDeployment{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "PulseProDeployment")
			os.Exit(1)
		}
	} else {
		setupLog.Info("Webhooks are disabled.")
	}

	// Add health and readiness checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// PulseProValues holds the hostname and other relevant config
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
		Host string `yaml:"host"`
	} `yaml:"timescaledb"`
	Postgres struct {
		Host string `yaml:"host"`
	} `yaml:"postgres"`
}

// loadConfig parses the ConfigMap data into PulseProValues
func loadConfig(data string) (*PulseProValues, error) {
	var values PulseProValues
	err := yaml.Unmarshal([]byte(data), &values)
	if err != nil {
		return nil, fmt.Errorf("failed to parse values from ConfigMap: %v", err)
	}
	return &values, nil
}

// checkConnectivity pings the hostnames from the values
func checkConnectivity(values *PulseProValues) error {
	services := map[string]string{
		"Vault":       values.Vault.Address,
		"MidTier":     values.Midtier.Host,
		"RabbitMQ":    values.RabbitMQ.Host,
		"TimescaleDB": values.TimescaleDB.Host,
		"Postgres":    values.Postgres.Host,
	}

	for service, host := range services {
		if host == "" {
			return fmt.Errorf("hostname for %s is empty in the ConfigMap", service)
		}
		cmd := exec.Command("ping", "-c", "1", host)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to connect to %s (%s): %v", service, host, err)
		}
		fmt.Printf("Successfully connected to %s (%s)\n", service, host)
	}
	return nil
}
