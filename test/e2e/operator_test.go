package e2e

import (
	"testing"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

// TestExtended runs the operator tests using standard Go testing.
func TestExtended(t *testing.T) {
	// Register Gomega with the testing framework for standard Go test mode
	o.RegisterTestingT(t)

	t.Run("SecondaryScheduler Operator", func(t *testing.T) {
		// Setup operator and wait for it to be ready
		ctx, cancelFnc, kubeClient, err := setupOperator(t)
		if err != nil {
			t.Fatalf("Failed to setup operator: %v", err)
		}
		defer cancelFnc()

		t.Run("Scheduling a pod", func(t *testing.T) {
			testNamespace := testScheduling(g.GinkgoTB(), ctx, kubeClient)
			defer cleanupTestNamespace(t, ctx, kubeClient, testNamespace)
		})

		// Get expected resources from bindata
		metricsService := getMetricsService()
		serviceMonitor := getServiceMonitor()
		deployment := getSecondarySchedulerDeployment()

		// Extract pod labels from deployment spec and validate there's exactly one label selector
		podLabels := deployment.Spec.Template.Labels
		if len(podLabels) != 1 {
			t.Fatalf("Expected exactly one label selector for secondary-scheduler pods, got %d", len(podLabels))
		}

		// Get the metrics port (must be exactly one port)
		if len(metricsService.Spec.Ports) != 1 {
			t.Fatalf("Expected exactly one port in metrics service, got %d", len(metricsService.Spec.Ports))
		}
		metricsPort := metricsService.Spec.Ports[0]

		t.Run("Metrics service exists", func(t *testing.T) {
			testMetricsServiceExists(t, ctx, kubeClient, metricsService.Name, metricsService.Labels, metricsPort)
		})

		t.Run("ServiceMonitor exists", func(t *testing.T) {
			testServiceMonitorExists(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Labels)
		})

		t.Run("Prometheus target is up", func(t *testing.T) {
			testPrometheusTargetUp(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Name, metricsService.Labels, metricsPort, podLabels)
		})

		t.Run("Metrics data available", func(t *testing.T) {
			testMetricsDataAvailable(t, ctx, kubeClient, podLabels)
		})
	})
}
