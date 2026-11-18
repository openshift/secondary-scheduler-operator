package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	olm "github.com/operator-framework/api/pkg/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	clocktesting "k8s.io/utils/clock/testing"
	utilpointer "k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	secondaryschedulerv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	secondaryschedulerscheme "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/secondary-scheduler-operator/test/e2e/bindata"
)

const (
	// NetworkPolicy test constants
	agnhostImage         = "registry.k8s.io/e2e-test-images/agnhost:2.66.1"
	metricsPort          = "10259"
	connectionTimeout    = "5s"
	monitoringNamespace  = "openshift-monitoring"
	testAllowedNamespace = "e2e-netpol-allowed-test"
	testDeniedNamespace  = "e2e-netpol-denied-test"
	monitoringLabelKey   = "openshift.io/cluster-monitoring"
	monitoringLabelValue = "true"
)

// extractClusterRoleFromCSV extracts the ClusterRole from CSV bytes
func extractClusterRoleFromCSV(csvBytes []byte) (*rbacv1.ClusterRole, error) {
	// Parse the CSV
	var csv olm.ClusterServiceVersion
	if err := yaml.Unmarshal(csvBytes, &csv); err != nil {
		return nil, fmt.Errorf("failed to unmarshal CSV: %w", err)
	}

	// Extract the first clusterPermissions rules
	if len(csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions) == 0 {
		return nil, fmt.Errorf("no clusterPermissions found in CSV")
	}

	rules := csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions[0].Rules

	// Create the ClusterRole
	clusterRole := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "secondary-scheduler-operator",
		},
		Rules: rules,
	}

	return clusterRole, nil
}

// extractRoleFromCSV extracts the Role from CSV bytes
func extractRoleFromCSV(csvBytes []byte) (*rbacv1.Role, error) {
	// Parse the CSV
	var csv olm.ClusterServiceVersion
	if err := yaml.Unmarshal(csvBytes, &csv); err != nil {
		return nil, fmt.Errorf("failed to unmarshal CSV: %w", err)
	}

	// Extract the first permissions rules
	if len(csv.Spec.InstallStrategy.StrategySpec.Permissions) == 0 {
		return nil, fmt.Errorf("no permissions found in CSV")
	}

	rules := csv.Spec.InstallStrategy.StrategySpec.Permissions[0].Rules

	// Create the Role
	role := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secondary-scheduler-operator",
			Namespace: operatorclient.OperatorNamespace,
			Annotations: map[string]string{
				"include.release.openshift.io/self-managed-high-availability": "true",
				"include.release.openshift.io/single-node-developer":          "true",
			},
		},
		Rules: rules,
	}

	return role, nil
}

// setupOperator sets up the operator and waits for it to be ready.
// This function works with both standard Go testing and Ginkgo.
func setupOperator(t testing.TB) (context.Context, context.CancelFunc, *k8sclient.Clientset, error) {
	ctx, cancelFnc := context.WithCancel(context.Background())

	// Verify required environment variables
	if os.Getenv("KUBECONFIG") == "" {
		return ctx, cancelFnc, nil, fmt.Errorf("KUBECONFIG environment variable must be set")
	}
	if os.Getenv("IMAGE") == "" {
		if os.Getenv("IMAGE_FORMAT") == "" {
			return ctx, cancelFnc, nil, fmt.Errorf("IMAGE_FORMAT environment variable must be set")
		}
		if os.Getenv("NAMESPACE") == "" {
			return ctx, cancelFnc, nil, fmt.Errorf("NAMESPACE environment variable must be set")
		}
	}

	// Initialize clients
	kubeClient := GetKubeClient()
	apiExtClient := GetApiExtensionClient()
	secondarySchedulerClient := GetSecondarySchedulerClient()

	eventRecorder := events.NewKubeRecorder(
		kubeClient.CoreV1().Events("default"),
		"test-e2e",
		&corev1.ObjectReference{},
		clocktesting.NewFakePassiveClock(time.Now()),
	)

	assets := []struct {
		path           string
		readerAndApply func(objBytes []byte) error
	}{
		{
			path: "assets/00_secondary-scheduler-operator.crd.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyCustomResourceDefinitionV1(ctx, apiExtClient.ApiextensionsV1(), eventRecorder, resourceread.ReadCustomResourceDefinitionV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/01_namespace.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadNamespaceV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/02_serviceaccount.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyServiceAccount(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadServiceAccountV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/cluster-secondary-scheduler-operator.clusterserviceversion.yaml",
			readerAndApply: func(objBytes []byte) error {
				clusterRole, err := extractClusterRoleFromCSV(objBytes)
				if err != nil {
					return fmt.Errorf("failed to extract ClusterRole from CSV: %w", err)
				}
				_, _, err = resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), eventRecorder, clusterRole)
				return err
			},
		},
		{
			path: "assets/cluster-secondary-scheduler-operator.clusterserviceversion.yaml",
			readerAndApply: func(objBytes []byte) error {
				role, err := extractRoleFromCSV(objBytes)
				if err != nil {
					return fmt.Errorf("failed to extract Role from CSV: %w", err)
				}
				_, _, err = resourceapply.ApplyRole(ctx, kubeClient.RbacV1(), eventRecorder, role)
				return err
			},
		},
		{
			path: "assets/04_clusterrolebinding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_kube-scheduler-cluster-role-binding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_volume-scheduler-cluster-role-binding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_prometheus-cluster-role-binding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_operatorrolebinding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/05_deployment.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadDeploymentV1OrDie(objBytes)
				// override the operator image with the one built in the CI

				// E.g. IMAGE_FORMAT=registry.build03.ci.openshift.org/ci-op-52fj47p4/stable:${component}
				registry := strings.Split(os.Getenv("IMAGE_FORMAT"), "/")[0]
				image := registry + "/" + os.Getenv("NAMESPACE") + "/pipeline:secondary-scheduler-operator"
				if os.Getenv("IMAGE") != "" {
					image = os.Getenv("IMAGE")
				}
				required.Spec.Template.Spec.Containers[0].Image = image
				_, _, err := resourceapply.ApplyDeployment(
					ctx,
					kubeClient.AppsV1(),
					eventRecorder,
					required,
					1000, // any random high number
				)
				return err
			},
		},
		{
			path: "assets/06_configmap.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyConfigMap(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadConfigMapV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/07_secondary-scheduler-operator.cr.yaml",
			readerAndApply: func(objBytes []byte) error {
				requiredObj, err := runtime.Decode(secondaryschedulerscheme.Codecs.UniversalDecoder(secondaryschedulerv1.SchemeGroupVersion), objBytes)
				if err != nil {
					klog.Errorf("Unable to decode assets/07_secondary-scheduler-operator.cr.yaml: %v", err)
					return err
				}
				requiredSS := requiredObj.(*secondaryschedulerv1.SecondaryScheduler)

				_, err = secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Create(ctx, requiredSS, metav1.CreateOptions{})
				if err == nil {
					return nil
				}
				if !apierrors.IsAlreadyExists(err) {
					return err
				}
				// Get the existing object to obtain its resourceVersion
				existingSS, getErr := secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Get(ctx, requiredSS.Name, metav1.GetOptions{})
				if getErr != nil {
					return getErr
				}
				// Update the spec with the required values
				existingSS.Spec = requiredSS.Spec
				_, err = secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Update(ctx, existingSS, metav1.UpdateOptions{})
				return err
			},
		},
	}

	// Apply all assets
	klog.Infof("Creating operator resources (namespace, CRD, RBAC, deployment)")
	o.Eventually(func() bool {
		allSucceeded := true
		for _, asset := range assets {
			klog.Infof("Creating %v", asset.path)
			if err := asset.readerAndApply(bindata.MustAsset(asset.path)); err != nil {
				klog.Errorf("Unable to create %v: %v", asset.path, err)
				allSucceeded = false
			}
		}
		return allSucceeded
	}, 10*time.Second, 1*time.Second).Should(o.BeTrue(), "failed to create assets")

	// Wait for operator pod to be running
	klog.Infof("Waiting for operator pod to be running")
	o.Eventually(func() bool {
		podItems, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, operatorclient.OperandName+"-") {
				continue
			}
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				klog.Infof("Operator pod %v is running", pod.Name)
				return true
			}
		}
		return false
	}, 1*time.Minute, 5*time.Second).Should(o.BeTrue(), "operator pod not running after timeout")

	klog.Infof("All operator components are running and ready")
	return ctx, cancelFnc, kubeClient, nil
}

// buildSchedulingTestPod builds a pause pod scheduled by the given scheduler.
func buildSchedulingTestPod(namespace, name, schedulerName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utilpointer.BoolPtr(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			SchedulerName: schedulerName,
			Containers: []corev1.Container{{
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: utilpointer.BoolPtr(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{
							"ALL",
						},
					},
				},
				Name:            "pause",
				ImagePullPolicy: "IfNotPresent",
				Image:           "registry.k8s.io/pause:3.10",
				Ports:           []corev1.ContainerPort{{ContainerPort: 80}},
			}},
		},
	}
}

// waitForPodScheduled waits until the pod is assigned to a node by its scheduler.
func waitForPodScheduled(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, namespace, podName string) {
	o.Eventually(func() bool {
		klog.Infof("Listing pods...")
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Unable to get pod: %v", err)
			return false
		}
		if pod.Spec.NodeName == "" {
			klog.Infof("Pod not yet assigned to a node")
			return false
		}
		klog.Infof("Pod successfully assigned to a node: %v", pod.Spec.NodeName)

		return true
	}, 2*time.Minute, 1*time.Second).Should(o.BeTrue(), "pod not scheduled after timeout")
}

func testScheduling(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) string {
	testNamespace := "e2e-test-secondaryschedulerscheduling"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}

	klog.Infof("Creating test namespace")
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test namespace: %v", err)
	}

	// Verify that pods are scheduled by the expected scheduler:
	tests := []struct {
		name              string
		podName           string
		schedulerName     string // empty means the schedulerName field is omitted
		expectedScheduler string
	}{
		{
			name:              "pod scheduled via secondary-scheduler",
			podName:           "test-secondary-scheduler-scheduling-pod",
			schedulerName:     "secondary-scheduler",
			expectedScheduler: "secondary-scheduler",
		},
		{
			name:              "pod scheduled via default-scheduler",
			podName:           "test-default-scheduler-scheduling-pod",
			schedulerName:     "default-scheduler",
			expectedScheduler: "default-scheduler",
		},
		{
			name:              "pod without schedulerName defaults to default-scheduler",
			podName:           "test-default-scheduler-implicit-pod",
			schedulerName:     "",
			expectedScheduler: "default-scheduler",
		},
	}

	for _, test := range tests {
		klog.Infof("Creating pod %s (schedulerName=%q)", test.podName, test.schedulerName)
		pod := buildSchedulingTestPod(testNamespace, test.podName, test.schedulerName)
		if _, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Unable to create a pod: %v", err)
		}

		waitForPodScheduled(t, ctx, kubeClient, pod.Namespace, pod.Name)

		scheduledPod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Unable to get pod %s: %v", pod.Name, err)
		}
		if scheduledPod.Spec.SchedulerName != test.expectedScheduler {
			t.Fatalf("Pod %s: expected schedulerName %q, got %q", pod.Name, test.expectedScheduler, scheduledPod.Spec.SchedulerName)
		}
		klog.Infof("Pod %s scheduled by %s on node %s", pod.Name, scheduledPod.Spec.SchedulerName, scheduledPod.Spec.NodeName)
	}

	return testNamespace
}

// cleanupTestNamespace deletes the test namespace.
func cleanupTestNamespace(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, testNamespace string) {
	if testNamespace == "" {
		return
	}
	klog.Infof("Cleaning up test namespace: %s", testNamespace)
	if err := kubeClient.CoreV1().Namespaces().Delete(ctx, testNamespace, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete namespace %s: %v", testNamespace, err)
		}
		// Don't fail the cleanup with Fatalf - just log and continue
		// This allows retries to work properly
		return
	}
	o.Eventually(func() bool {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, testNamespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true
		}
		return false
	}, time.Minute, 1*time.Second).Should(o.BeTrue(), "namespace not deleted after timeout")
}

// testMetricsServiceExists verifies that the metrics service exists
func testMetricsServiceExists(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceName string, expectedLabels map[string]string, expectedPort corev1.ServicePort) {
	klog.Infof("Verifying metrics service exists")

	// Get the actual service from the cluster
	service, err := kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Get(ctx, serviceName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "metrics service should exist")

	// Verify service has the correct labels
	for key, value := range expectedLabels {
		o.Expect(service.Labels).To(o.HaveKeyWithValue(key, value), "service should have label %s=%s", key, value)
	}

	// Verify service exposes the expected port
	o.Expect(service.Spec.Ports).NotTo(o.BeEmpty(), "service should have at least one port")
	found := false
	for _, actualPort := range service.Spec.Ports {
		if actualPort.Name == expectedPort.Name && actualPort.Port == expectedPort.Port {
			found = true
			break
		}
	}
	o.Expect(found).To(o.BeTrue(), "service should expose port %s on port %d", expectedPort.Name, expectedPort.Port)

	klog.Infof("Metrics service verified successfully")
}

// testServiceMonitorExists verifies that the ServiceMonitor exists and its selector matches the service labels
func testServiceMonitorExists(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceMonitorName string, expectedServiceLabels map[string]string) {
	klog.Infof("Verifying ServiceMonitor exists")

	// Get monitoring client to access ServiceMonitor (CRD from prometheus-operator)
	monitoringClient := GetMonitoringClient()

	// Get the ServiceMonitor
	serviceMonitor, err := monitoringClient.MonitoringV1().ServiceMonitors(operatorclient.OperatorNamespace).
		Get(ctx, serviceMonitorName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "ServiceMonitor should exist")

	// Verify ServiceMonitor has the correct spec
	o.Expect(serviceMonitor.Spec.Selector).NotTo(o.BeNil(), "ServiceMonitor should have selector")
	o.Expect(serviceMonitor.Spec.Selector.MatchLabels).NotTo(o.BeNil(), "selector should have matchLabels")

	// Verify ServiceMonitor selector matches the service labels
	for key, value := range expectedServiceLabels {
		o.Expect(serviceMonitor.Spec.Selector.MatchLabels).To(o.HaveKeyWithValue(key, value), "ServiceMonitor selector should match service label %s=%s", key, value)
	}

	klog.Infof("ServiceMonitor verified successfully")
}

// testPrometheusTargetUp verifies that the Prometheus target is up and scraping metrics
func testPrometheusTargetUp(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceMonitorName string, metricsServiceName string, serviceLabels map[string]string, metricsPort corev1.ServicePort, podLabels map[string]string) {
	klog.Infof("Verifying Prometheus target is up")

	// Get the Prometheus token for authentication
	token, err := getPrometheusToken(ctx, kubeClient, podLabels)
	o.Expect(err).NotTo(o.HaveOccurred(), "should get Prometheus token")

	// Get the Route client
	routeClient := GetRouteClient()

	// Query Prometheus targets API
	klog.Infof("Querying Prometheus for secondary-scheduler target status")

	o.Eventually(func() bool {
		// First, get all EndpointSlices matching the service labels
		endpointAddresses, err := getEndpointAddressesForService(ctx, kubeClient, operatorclient.OperatorNamespace, serviceLabels)
		if err != nil {
			klog.Errorf("Failed to get endpoint addresses: %v", err)
			return false
		}

		if len(endpointAddresses) == 0 {
			klog.Infof("No endpoints found for service yet, waiting...")
			return false
		}

		klog.Infof("Found %d endpoint(s) for service", len(endpointAddresses))

		// Query each endpoint individually
		allEndpointsHealthy := true
		for _, endpointIP := range endpointAddresses {
			// Construct the instance string as "IP:port"
			instance := fmt.Sprintf("%s:%d", endpointIP, metricsPort.TargetPort.IntVal)

			klog.Infof("Querying Prometheus for instance: %s", instance)

			result, err := queryPrometheusTarget(ctx, kubeClient, routeClient, token, serviceMonitorName, instance)
			if err != nil {
				klog.Errorf("Failed to query Prometheus target for instance %s: %v", instance, err)
				allEndpointsHealthy = false
				continue
			}

			// Check if we got any results (target exists)
			if len(result.Data.Result) == 0 {
				klog.Infof("Target for instance %s not found yet in Prometheus, waiting...", instance)
				allEndpointsHealthy = false
				continue
			}

			// The 'up' metric returns 1 if target is healthy, 0 if down
			series := result.Data.Result[0]
			if len(series.Value) < 2 {
				klog.Warningf("Instance %s: unexpected metric value format: %v", instance, series.Value)
				allEndpointsHealthy = false
				continue
			}

			valueStr, ok := series.Value[1].(string)
			if !ok {
				klog.Warningf("Instance %s: metric value is not a string: %v", instance, series.Value[1])
				allEndpointsHealthy = false
				continue
			}

			klog.Infof("Instance %s: up=%s, labels=%v", instance, valueStr, series.Metric)

			if valueStr != "1" {
				klog.Warningf("Instance %s is not up yet", instance)
				allEndpointsHealthy = false
			} else {
				klog.Infof("Instance %s is healthy", instance)
			}
		}

		if allEndpointsHealthy {
			klog.Infof("All endpoints have healthy Prometheus targets")
			return true
		}

		klog.Warningf("Not all endpoints are healthy yet, waiting...")
		return false
	}, 5*time.Minute, 10*time.Second).Should(o.BeTrue(), "Prometheus target should be up")

	klog.Infof("Prometheus target verified successfully")
}

// testMetricsDataAvailable verifies that specific metrics are available and have data
func testMetricsDataAvailable(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, podLabels map[string]string) {
	klog.Infof("Verifying metrics data is available")

	// Get the Prometheus token for authentication
	token, err := getPrometheusToken(ctx, kubeClient, podLabels)
	o.Expect(err).NotTo(o.HaveOccurred(), "should get Prometheus token")

	// Get the Route client
	routeClient := GetRouteClient()

	// Query for the specific metric
	metricQuery := `scheduler_pod_scheduling_attempts_bucket{container="secondary-scheduler"}`

	klog.Infof("Querying Prometheus for metric: %s", metricQuery)

	o.Eventually(func() bool {
		result, err := queryPrometheusMetric(ctx, kubeClient, routeClient, token, metricQuery)
		if err != nil {
			klog.Errorf("Failed to query Prometheus metric: %v", err)
			return false
		}

		// Check if we have any results
		if len(result.Data.Result) == 0 {
			klog.Infof("No results found for metric %s yet, waiting...", metricQuery)
			return false
		}

		// Verify we have data
		klog.Infof("Found %d metric series for %s", len(result.Data.Result), metricQuery)
		for i, series := range result.Data.Result {
			if len(series.Value) > 0 {
				klog.Infof("Series %d: metric=%v, value=%v", i, series.Metric, series.Value)
			}
		}

		return true
	}, 5*time.Minute, 10*time.Second).Should(o.BeTrue(), "Metric data should be available")

	klog.Infof("Metrics data verified successfully")
}

// testNetworkPolicyForOperand validates the secondary-scheduler-operand NetworkPolicy
func testNetworkPolicyForOperand(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	testSecondarySchedulerOperandNetworkPolicy(t, ctx, kubeClient)

	klog.Infof("Testing NetworkPolicy reconciliation (recreation after deletion)")
	testNetworkPolicyRecreation(t, ctx, kubeClient, "secondary-scheduler-operand")

	klog.Infof("Testing NetworkPolicy reconciliation (reversion of manual changes)")
	testNetworkPolicyReversion(t, ctx, kubeClient, "secondary-scheduler-operand")
}

// validateEgressAllowAll verifies that egress rules allow all traffic (single empty rule)
func validateEgressAllowAll(np *networkingv1.NetworkPolicy) {
	o.Expect(np.Spec.Egress).To(o.HaveLen(1), "Expected exactly one egress rule")
	egressRule := np.Spec.Egress[0]
	o.Expect(egressRule.To).To(o.BeEmpty(), "Egress rule should have no 'to' restrictions (allow all)")
	o.Expect(egressRule.Ports).To(o.BeEmpty(), "Egress rule should have no port restrictions (allow all)")
}

// networkPolicyConnTest describes a connectivity test scenario for NetworkPolicy validation
type networkPolicyConnTest struct {
	name              string
	namespace         *corev1.Namespace
	expectSuccess     bool
	podName           string
	ginkgoDescription string
}

func testSecondarySchedulerOperandNetworkPolicy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	networkPolicyName := "secondary-scheduler-operand"

	networkPolicy, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, networkPolicyName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "NetworkPolicy %s should exist in namespace %s", networkPolicyName, operatorclient.OperatorNamespace)
	klog.Infof("NetworkPolicy %s exists in namespace %s", networkPolicy.Name, operatorclient.OperatorNamespace)

	o.Expect(networkPolicy.Spec.PolicyTypes).To(o.ContainElement(networkingv1.PolicyTypeEgress), "policyTypes should include Egress")
	validateEgressAllowAll(networkPolicy)
	klog.Infof("NetworkPolicy egress rules correctly configured to allow all traffic")

	secondarySchedulerService := fmt.Sprintf("metrics.%s.svc:%s", operatorclient.OperatorNamespace, metricsPort)

	tests := []networkPolicyConnTest{
		{
			name:              "openshift-monitoring",
			namespace:         nil,
			expectSuccess:     true,
			podName:           "e2e-netpol-test-monitoring",
			ginkgoDescription: "Testing connection from openshift-monitoring namespace (should succeed)",
		},
		{
			name: "labeled-namespace",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testAllowedNamespace,
					Labels: map[string]string{
						monitoringLabelKey: monitoringLabelValue,
					},
				},
			},
			expectSuccess:     true,
			podName:           "e2e-netpol-test-allowed",
			ginkgoDescription: "Testing connection from labeled namespace (should succeed)",
		},
		{
			name: "unauthorized-namespace",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testDeniedNamespace,
				},
			},
			expectSuccess:     false,
			podName:           "e2e-netpol-test-denied",
			ginkgoDescription: "Testing connection from unauthorized namespace (should fail)",
		},
	}

	for _, tc := range tests {
		func(tc networkPolicyConnTest) {
			klog.Info(tc.ginkgoDescription)

			namespaceName := monitoringNamespace
			if tc.namespace != nil {
				namespaceName = tc.namespace.Name
				_, err := kubeClient.CoreV1().Namespaces().Create(ctx, tc.namespace, metav1.CreateOptions{})
				o.Expect(err).NotTo(o.HaveOccurred(), "Should create namespace %s", namespaceName)
				defer cleanupTestNamespace(t, ctx, kubeClient, namespaceName)
			}

			pod := createAgnhostConnectPod(ctx, t, kubeClient, namespaceName, tc.podName, secondarySchedulerService, connectionTimeout)
			defer deletePodAndWait(ctx, t, kubeClient, namespaceName, pod.Name)

			err := waitForPodCompletion(ctx, t, kubeClient, namespaceName, pod.Name, tc.expectSuccess)
			o.Expect(err).NotTo(o.HaveOccurred(), "Connection test '%s' failed", tc.name)
			klog.Infof("Test '%s' passed", tc.name)
		}(tc)
	}

	klog.Infof("NetworkPolicy ingress rules validated successfully")
}

// createAgnhostConnectPod creates an agnhost pod that runs the connect command
func createAgnhostConnectPod(ctx context.Context, t testing.TB, kubeClient *k8sclient.Clientset, namespace, name, target, timeout string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "e2e-netpol-test"},
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utilpointer.BoolPtr(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "agnhost",
					Image:           agnhostImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args: []string{
						"connect",
						target,
						fmt.Sprintf("--timeout=%s", timeout),
						"--protocol=tcp",
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: utilpointer.BoolPtr(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	createdPod, err := kubeClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "Should create agnhost connect pod in namespace %s", namespace)
	klog.Infof("Created agnhost connect pod %s/%s with command: connect %s --timeout=%s --protocol=tcp", namespace, name, target, timeout)

	return createdPod
}

// waitForPodCompletion waits for a pod to complete and checks if it succeeded or failed
// expectSuccess indicates whether the pod should succeed (exit 0) or fail (exit non-zero)
func waitForPodCompletion(ctx context.Context, t testing.TB, kubeClient *k8sclient.Clientset, namespace, podName string, expectSuccess bool) error {
	klog.Infof("Waiting for pod %s/%s to complete (expectSuccess=%v)", namespace, podName, expectSuccess)

	var finalPod *corev1.Pod
	o.Eventually(func() bool {
		p, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			klog.V(2).Infof("Error getting pod %s/%s: %v", namespace, podName, err)
			return false
		}
		klog.V(2).Infof("Pod %s/%s phase: %s", namespace, podName, p.Status.Phase)
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			finalPod = p
			return true
		}
		return false
	}).WithTimeout(30*time.Second).WithPolling(2*time.Second).Should(o.BeTrue(), "Pod should complete")

	succeeded := finalPod.Status.Phase == corev1.PodSucceeded

	if !succeeded {
		logs, err := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{}).Do(ctx).Raw()
		if err == nil && len(logs) > 0 {
			logStr := string(logs)
			// Check for agnhost error prefixes that indicate non-policy failures
			// See https://github.com/kubernetes/kubernetes/blob/master/test/images/agnhost/README.md#connect
			if strings.HasPrefix(logStr, "DNS") || strings.HasPrefix(logStr, "REFUSED") ||
				strings.HasPrefix(logStr, "UNKNOWN") || strings.HasPrefix(logStr, "OTHER") {
				return fmt.Errorf("pod failed with non-policy error (expectSuccess=%v): %s", expectSuccess, logStr)
			}
		}
	}

	if succeeded != expectSuccess {
		detail := ""
		if len(finalPod.Status.ContainerStatuses) > 0 && finalPod.Status.ContainerStatuses[0].State.Terminated != nil {
			t := finalPod.Status.ContainerStatuses[0].State.Terminated
			detail = fmt.Sprintf(": exit code %d, reason: %s", t.ExitCode, t.Reason)
		}
		return fmt.Errorf("expected pod to have succeeded=%v but got %s%s", expectSuccess, finalPod.Status.Phase, detail)
	}

	klog.Infof("Pod %s/%s phase %s as expected", namespace, podName, finalPod.Status.Phase)
	return nil
}

// deletePodAndWait deletes a pod and waits for it to be removed
func deletePodAndWait(ctx context.Context, t testing.TB, kubeClient *k8sclient.Clientset, namespace, podName string) {
	klog.Infof("Deleting pod %s/%s", namespace, podName)
	err := kubeClient.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Warningf("Failed to delete pod %s/%s: %v", namespace, podName, err)
	}

	o.Eventually(func() bool {
		_, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true
		}
		return false
	}).WithTimeout(1*time.Minute).WithPolling(2*time.Second).Should(o.BeTrue(), "Pod should be deleted")
}

// testNetworkPolicyRecreation verifies that the operator recreates NetworkPolicy if deleted
func testNetworkPolicyRecreation(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, networkPolicyName string) {
	err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Delete(ctx, networkPolicyName, metav1.DeleteOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	o.Eventually(func() bool {
		_, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, networkPolicyName, metav1.GetOptions{})
		return err == nil
	}).WithTimeout(1*time.Minute).WithPolling(2*time.Second).Should(o.BeTrue(), "Operator should recreate NetworkPolicy")
}

// testNetworkPolicyReversion verifies that the operator reverts manual changes to NetworkPolicy
// NOTE: This is not a comprehensive test. Complete validation of operator reconciliation logic
// is done through unit tests. This e2e test only exercises the minimal mechanics of triggering
// reconciliation through event handlers.
func testNetworkPolicyReversion(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, networkPolicyName string) {
	np, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, networkPolicyName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	originalResourceVersion := np.ResourceVersion
	originalIngressRuleCount := len(np.Spec.Ingress)

	np.Spec.Ingress = append(np.Spec.Ingress, networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test.e2e/manual": "should-revert",
					},
				},
			},
		},
	})

	updatedNP, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Update(ctx, np, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	modifiedResourceVersion := updatedNP.ResourceVersion

	o.Eventually(func() bool {
		np, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, networkPolicyName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return len(np.Spec.Ingress) == originalIngressRuleCount && np.ResourceVersion != modifiedResourceVersion && np.ResourceVersion != originalResourceVersion
	}).WithTimeout(1*time.Minute).WithPolling(2*time.Second).Should(o.BeTrue(), "Operator should revert manual changes")
}

// Ginkgo test specs for OTE framework - calls the shared test functions
var _ = g.Describe("[sig-scheduling][Operator][Serial] SecondaryScheduler Operator", g.Ordered, func() {
	var (
		ctx           context.Context
		cancelFnc     context.CancelFunc
		kubeClient    *k8sclient.Clientset
		testNamespace string
	)

	g.BeforeAll(func() {
		g.By("Setting up the operator")
		var err error
		ctx, cancelFnc, kubeClient, err = setupOperator(g.GinkgoTB())
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.AfterAll(func() {
		if cancelFnc != nil {
			cancelFnc()
		}
	})

	g.Context("when secondary scheduler is deployed", func() {
		g.It("should schedule pods using the secondary and default schedulers", func() {
			testNamespace = testScheduling(g.GinkgoTB(), ctx, kubeClient)
			g.DeferCleanup(func() {
				cleanupTestNamespace(g.GinkgoTB(), ctx, kubeClient, testNamespace)
			})
		})
	})

	g.Describe("NetworkPolicy", g.Ordered, func() {
		g.It("should have NetworkPolicy for operand configured", func() {
			testNetworkPolicyForOperand(g.GinkgoTB(), ctx, kubeClient)
		})
	})

	g.Describe("Observability", g.Ordered, func() {
		var (
			metricsService *corev1.Service
			serviceMonitor *monitoringv1.ServiceMonitor
			deployment     *appsv1.Deployment
			metricsPort    corev1.ServicePort
			podLabels      map[string]string
		)

		g.BeforeAll(func() {
			// Get expected resources from bindata
			metricsService = getMetricsService()
			serviceMonitor = getServiceMonitor()
			deployment = getSecondarySchedulerDeployment()

			// Extract pod labels from deployment spec and validate there's exactly one label selector
			podLabels = deployment.Spec.Template.Labels
			o.Expect(podLabels).To(o.HaveLen(1), "Expected exactly one label selector for secondary-scheduler pods")

			// Get the metrics port (must be exactly one port)
			o.Expect(metricsService.Spec.Ports).To(o.HaveLen(1), "Expected exactly one port in metrics service")
			metricsPort = metricsService.Spec.Ports[0]
		})

		g.It("should have a metrics service", func() {
			testMetricsServiceExists(g.GinkgoTB(), ctx, kubeClient, metricsService.Name, metricsService.Labels, metricsPort)
		})

		g.It("should have a ServiceMonitor", func() {
			testServiceMonitorExists(g.GinkgoTB(), ctx, kubeClient, serviceMonitor.Name, metricsService.Labels)
		})

		g.It("should have Prometheus target up", func() {
			testPrometheusTargetUp(g.GinkgoTB(), ctx, kubeClient, serviceMonitor.Name, metricsService.Name, metricsService.Labels, metricsPort, podLabels)
		})

		g.It("should have metrics data available", func() {
			testMetricsDataAvailable(g.GinkgoTB(), ctx, kubeClient, podLabels)
		})
	})
})
