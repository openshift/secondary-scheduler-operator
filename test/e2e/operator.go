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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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
// This function is idempotent - if operator is already deployed, it skips setup and returns early.
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

	// Early return if operator is already deployed and running
	if deploy, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler-operator", metav1.GetOptions{}); err == nil && deploy.Status.ReadyReplicas > 0 {
		klog.Infof("Operator already deployed and running, skipping setup")
		return ctx, cancelFnc, kubeClient, nil
	}

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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "test-secondary-scheduler-scheduling-pod",
			Labels:    map[string]string{"app": "test-secondary-scheduler-scheduling"},
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utilpointer.BoolPtr(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			SchedulerName: "secondary-scheduler",
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
	if _, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Unable to create a pod: %v", err)
	}

	o.Eventually(func() bool {
		klog.Infof("Listing pods...")
		pod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
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
	}, 2*time.Minute, 1*time.Second).Should(o.BeTrue(), "pod not running after timeout")

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
		g.It("should schedule a pod using the secondary scheduler", func() {
			testNamespace = testScheduling(g.GinkgoTB(), ctx, kubeClient)
			g.DeferCleanup(func() {
				cleanupTestNamespace(g.GinkgoTB(), ctx, kubeClient, testNamespace)
			})
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

const (
	operandNetworkPolicyName = "secondary-scheduler-operand"
	operandAppLabelKey       = "app"
	operatorDeploymentName   = "secondary-scheduler-operator"

	metricsPort int32 = 10259
	unusedPort  int32 = 8080

	clusterMonitoringLabel = "openshift.io/cluster-monitoring"

	openshiftMonitoringNamespace  = "openshift-monitoring"
	openshiftUWMNamespace         = "openshift-user-workload-monitoring"
	openshiftDNSNamespace         = "openshift-dns"
	prometheusK8sServiceName      = "prometheus-k8s"
	connectivityTimeout           = 2 * time.Minute
	networkPolicyReconcileTimeout = 10 * time.Minute
)

var _ = g.Describe("[sig-scheduling][Operator][Serial] SecondaryScheduler NetworkPolicy", g.Ordered, func() {
	var (
		ctx        context.Context
		cancelFnc  context.CancelFunc
		kubeClient *k8sclient.Clientset
	)

	g.BeforeAll(func() {
		g.By("Setting up the secondary scheduler operator")
		var err error
		ctx, cancelFnc, kubeClient, err = setupOperator(g.GinkgoTB())
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Waiting for the operand NetworkPolicy and operand pod")
		waitForOperandNetworkPolicy(ctx, kubeClient)
		waitForOperandPod(ctx, kubeClient)
	})

	g.AfterAll(func() {
		if cancelFnc != nil {
			cancelFnc()
		}
	})

	g.It("should define operand NetworkPolicy with correct structure and selectors", func() {
		expectNetworkPolicyValid(ctx, kubeClient, "NetworkPolicy should exist with correct structure and selectors")
	})

	g.It("should enforce ingress policy for metrics port", func() {
		operandPod := waitForOperandPod(ctx, kubeClient)
		podIPs := []string{operandPod.Status.PodIP}
		testLabels := map[string]string{"test": "secondary-scheduler-netpol"}

		ingressTests := []struct {
			description string
			namespace   string
			port        int32
			shouldAllow bool
		}{
			{
				description: "metrics port allowed from openshift-monitoring",
				namespace:   openshiftMonitoringNamespace,
				port:        metricsPort,
				shouldAllow: true,
			},
			{
				description: "metrics port allowed from openshift-user-workload-monitoring",
				namespace:   openshiftUWMNamespace,
				port:        metricsPort,
				shouldAllow: true,
			},
			{
				description: "metrics port allowed from operator namespace (cluster-monitoring label)",
				namespace:   operatorclient.OperatorNamespace,
				port:        metricsPort,
				shouldAllow: true,
			},
			{
				description: "metrics port blocked from default namespace",
				namespace:   "default",
				port:        metricsPort,
				shouldAllow: false,
			},
			{
				description: "wrong port blocked from monitoring namespace",
				namespace:   openshiftMonitoringNamespace,
				port:        unusedPort,
				shouldAllow: false,
			},
		}

		for _, tc := range ingressTests {
			g.By(fmt.Sprintf("%s", tc.description))
			expectConnectivity(ctx, kubeClient, tc.namespace, testLabels, podIPs, tc.port, tc.shouldAllow)
		}
	})

	g.It("should allow kubelet/host-network to bypass NetworkPolicy", func() {
		operandPod := waitForOperandPod(ctx, kubeClient)
		podIPs := []string{operandPod.Status.PodIP}

		g.By("Kubelet/host-network should bypass metrics port policy")
		expectHostNetworkConnectivity(ctx, kubeClient, openshiftMonitoringNamespace, operandPod.Spec.NodeName, podIPs, metricsPort, true)
	})

	g.It("should allow unrestricted egress for DNS, API server, and prometheus", func() {
		clientLabels := operandClientLabels()

		dnsSvc, err := kubeClient.CoreV1().Services(openshiftDNSNamespace).Get(ctx, "dns-default", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		dnsIPs := serviceClusterIPs(dnsSvc)

		g.By("DNS: Allowed to openshiftDNSNamespace/dns-default:53")
		expectConnectivity(ctx, kubeClient, operatorclient.OperatorNamespace, clientLabels, dnsIPs, 53, true)

		kubeSvc, err := kubeClient.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		kubeIPs := serviceClusterIPs(kubeSvc)

		g.By("Kubernetes API: Allowed to default/kubernetes:443")
		expectConnectivity(ctx, kubeClient, operatorclient.OperatorNamespace, clientLabels, kubeIPs, 443, true)

		promSvc, err := kubeClient.CoreV1().Services(openshiftMonitoringNamespace).Get(ctx, prometheusK8sServiceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		promPort := int32(9091)
		for _, p := range promSvc.Spec.Ports {
			if p.Name == "web" {
				promPort = p.Port
				break
			}
		}
		promIPs := serviceClusterIPs(promSvc)

		g.By(fmt.Sprintf("Prometheus: Allowed to prometheus-k8s:%d", promPort))
		expectConnectivity(ctx, kubeClient, operatorclient.OperatorNamespace, clientLabels, promIPs, promPort, true)
	})

	g.It("should reconcile policy mutations and restore deleted policies", func() {
		expected := getNetworkPolicy(ctx, kubeClient, operatorclient.OperatorNamespace, operandNetworkPolicyName)

		g.By("Testing reconciliation of port mutation")
		patch := []byte(`[{"op":"replace","path":"/spec/ingress/0/ports/0/port","value":9999}]`)
		testMutationRecovery(ctx, kubeClient, patch, networkPolicyReconcileTimeout)

		expectNetworkPolicyValid(ctx, kubeClient, "operator should revert port mutation")

		g.By("Testing deletion and recreation of policy")
		restoreNetworkPolicy(ctx, kubeClient, expected, networkPolicyReconcileTimeout)

		logNetworkPolicyEvents(ctx, kubeClient, []string{operatorclient.OperatorNamespace}, operandNetworkPolicyName)
	})

	g.It("should recover NetworkPolicy after config drift on operator restart", func() {
		netpolClient := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace)

		g.By("Scaling down operator to 0 replicas")
		scaleDeployment(ctx, kubeClient, operatorclient.OperatorNamespace, operatorDeploymentName, 0)
		g.DeferCleanup(func() {
			scaleDeployment(ctx, kubeClient, operatorclient.OperatorNamespace, operatorDeploymentName, 1)
		})
		verifyPodCount(ctx, kubeClient, operatorclient.OperatorNamespace, "name="+operatorDeploymentName, 0)

		g.By("Wiping all ingress rules from NetworkPolicy")
		patch := []byte(`[{"op": "replace", "path": "/spec/ingress", "value": []}]`)
		_, err := netpolClient.Patch(ctx, operandNetworkPolicyName, "application/json-patch+json", patch, metav1.PatchOptions{})
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to wipe ingress rules")

		np, err := netpolClient.Get(ctx, operandNetworkPolicyName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to get NetworkPolicy after wipe")
		o.Expect(np.Spec.Ingress).To(o.BeEmpty(), "expected 0 ingress rules after wipe")

		g.By("Scaling operator back up to 1 replica")
		scaleDeployment(ctx, kubeClient, operatorclient.OperatorNamespace, operatorDeploymentName, 1)

		o.Eventually(func() error {
			deploy, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if deploy.Status.ReadyReplicas < 1 {
				return fmt.Errorf("operator not ready yet: %d ready replicas", deploy.Status.ReadyReplicas)
			}
			return nil
		}, 2*time.Minute, 2*time.Second).Should(o.Succeed(), "operator should become ready")

		expectNetworkPolicyValid(ctx, kubeClient, "operator should recover NetworkPolicy after restart")
	})
})

// expectNetworkPolicyValid waits for NetworkPolicy to exist and be valid
func expectNetworkPolicyValid(ctx context.Context, kubeClient k8sclient.Interface, msg string) {
	o.Eventually(func() error {
		netpol, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, operandNetworkPolicyName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		return validateNetworkPolicySpec(netpol)
	}, 1*time.Minute, 2*time.Second).Should(o.Succeed(), msg)
}

// validateNetworkPolicySpec validates the complete NetworkPolicy specification
func validateNetworkPolicySpec(netpol *networkingv1.NetworkPolicy) error {
	if netpol.Namespace != operatorclient.OperatorNamespace {
		return fmt.Errorf("policy namespace should be %s, got %s", operatorclient.OperatorNamespace, netpol.Namespace)
	}

	sel := netpol.Spec.PodSelector
	if sel.MatchLabels[operandAppLabelKey] != operatorclient.OperandName {
		return fmt.Errorf("invalid podSelector label: expected %s=%s, got %v", operandAppLabelKey, operatorclient.OperandName, sel.MatchLabels)
	}

	metricsPortFound := false
	for _, rule := range netpol.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP {
				continue
			}
			if p.Port != nil && p.Port.IntValue() == int(metricsPort) {
				metricsPortFound = true
			}
		}
	}

	if !metricsPortFound {
		return fmt.Errorf("metrics port %d not found in ingress rules", metricsPort)
	}

	clusterMonitoringFound := false
	monitoringNamespaceFound := false
	uwmNamespaceFound := false

	for _, rule := range netpol.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels != nil {
				if peer.NamespaceSelector.MatchLabels[clusterMonitoringLabel] == "true" {
					clusterMonitoringFound = true
				}
				if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == openshiftMonitoringNamespace {
					monitoringNamespaceFound = true
				}
				if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == openshiftUWMNamespace {
					uwmNamespaceFound = true
				}
			}
		}
	}

	if !clusterMonitoringFound {
		return fmt.Errorf("expected ingress from cluster-monitoring label not found")
	}
	if !monitoringNamespaceFound {
		return fmt.Errorf("expected ingress from namespace %s not found", openshiftMonitoringNamespace)
	}
	if !uwmNamespaceFound {
		return fmt.Errorf("expected ingress from namespace %s not found", openshiftUWMNamespace)
	}

	egressUnrestrictedFound := false
	for _, rule := range netpol.Spec.Egress {
		if len(rule.Ports) == 0 && len(rule.To) == 0 {
			egressUnrestrictedFound = true
			break
		}
	}
	if !egressUnrestrictedFound {
		return fmt.Errorf("unrestricted egress rule not found")
	}

	if !contains(netpol.Spec.PolicyTypes, networkingv1.PolicyTypeIngress) {
		return fmt.Errorf("PolicyTypeIngress not found in policyTypes")
	}
	if !contains(netpol.Spec.PolicyTypes, networkingv1.PolicyTypeEgress) {
		return fmt.Errorf("PolicyTypeEgress not found in policyTypes")
	}

	ownerFound := false
	for _, ref := range netpol.OwnerReferences {
		if ref.APIVersion == "operator.openshift.io/v1" && ref.Kind == "SecondaryScheduler" && ref.Name == operatorclient.OperatorConfigName {
			ownerFound = true
			break
		}
	}
	if !ownerFound {
		return fmt.Errorf("expected owner reference not found")
	}

	return nil
}

func contains(types []networkingv1.PolicyType, pType networkingv1.PolicyType) bool {
	for _, t := range types {
		if t == pType {
			return true
		}
	}
	return false
}

func serviceClusterIPs(svc *corev1.Service) []string {
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return nil
	}
	return []string{svc.Spec.ClusterIP}
}

func runConnectivityCheck(ctx context.Context, kubeClient k8sclient.Interface, namespace string, labels map[string]string, serverIP string, port int32, hostNetwork bool, nodeName string) (bool, error) {
	userID := int64(1001)
	allowPrivilegeEscalation := false
	runAsNonRoot := true

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "np-client-",
			Namespace:    namespace,
			Labels: map[string]string{
				"test": "connectivity",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				RunAsUser:    &userID,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "connect",
					Image:           "registry.k8s.io/e2e-test-images/agnhost:2.45",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsNonRoot:             &runAsNonRoot,
						RunAsUser:                &userID,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					Command: []string{"/agnhost"},
					Args: []string{
						"connect",
						"--protocol=tcp",
						"--timeout=5s",
						fmt.Sprintf("%s:%d", serverIP, port),
					},
				},
			},
			NodeName:    nodeName,
			HostNetwork: hostNetwork,
		},
	}

	if labels != nil {
		for k, v := range labels {
			pod.Labels[k] = v
		}
	}

	if hostNetwork {
		pod.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
	}

	created, err := kubeClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	podName := created.Name
	defer func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = kubeClient.CoreV1().Pods(namespace).Delete(deleteCtx, podName, metav1.DeleteOptions{})
	}()

	if err := waitForPodCompletion(ctx, kubeClient, namespace, podName); err != nil {
		return false, err
	}
	completed, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if len(completed.Status.ContainerStatuses) == 0 {
		return false, fmt.Errorf("no container status recorded for pod %s", podName)
	}
	terminated := completed.Status.ContainerStatuses[0].State.Terminated
	if terminated == nil {
		return false, fmt.Errorf("container in pod %s has not terminated", podName)
	}
	return terminated.ExitCode == 0, nil
}

func expectConnectivity(ctx context.Context, kubeClient k8sclient.Interface, namespace string, clientLabels map[string]string, serverIPs []string, port int32, shouldSucceed bool) {
	for _, ip := range serverIPs {
		g.By(fmt.Sprintf("checking IPv4 connectivity %s -> %s:%d expected=%t", namespace, ip, port, shouldSucceed))
		err := pollConnectivity(ctx, kubeClient, namespace, clientLabels, ip, port, shouldSucceed, false, "", connectivityTimeout)
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("connectivity check failed for %s -> %s:%d (expected %t)", namespace, ip, port, shouldSucceed))
	}
}

func expectHostNetworkConnectivity(ctx context.Context, kubeClient k8sclient.Interface, namespace, nodeName string, serverIPs []string, port int32, shouldSucceed bool) {
	for _, ip := range serverIPs {
		g.By(fmt.Sprintf("checking IPv4 host-network connectivity node=%s -> %s:%d expected=%t", nodeName, ip, port, shouldSucceed))
		err := pollConnectivity(ctx, kubeClient, namespace, nil, ip, port, shouldSucceed, true, nodeName, connectivityTimeout)
		o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("host-network connectivity check failed for node=%s -> %s:%d (expected %t)", nodeName, ip, port, shouldSucceed))
	}
}

func pollConnectivity(ctx context.Context, kubeClient k8sclient.Interface, namespace string, clientLabels map[string]string, serverIP string, port int32, shouldSucceed, hostNetwork bool, nodeName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(_ context.Context) (bool, error) {
		succeeded, err := runConnectivityCheck(ctx, kubeClient, namespace, clientLabels, serverIP, port, hostNetwork, nodeName)
		if err != nil {
			klog.V(4).Infof("connectivity check failed (will retry): %v", err)
			return false, nil
		}
		return succeeded == shouldSucceed, nil
	})
}

func waitForPodCompletion(ctx context.Context, kubeClient k8sclient.Interface, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed, nil
	})
}

func getNetworkPolicy(ctx context.Context, client k8sclient.Interface, namespace, name string) *networkingv1.NetworkPolicy {
	policy, err := client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("failed to get NetworkPolicy %s/%s", namespace, name))
	return policy
}

func restoreNetworkPolicy(ctx context.Context, client k8sclient.Interface, expected *networkingv1.NetworkPolicy, timeout time.Duration) {
	namespace := expected.Namespace
	name := expected.Name
	g.By(fmt.Sprintf("deleting NetworkPolicy %s/%s and waiting for restoration", namespace, name))
	err := client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("failed to delete NetworkPolicy %s/%s", namespace, name))

	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		current, err := client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return apiequality.Semantic.DeepEqual(expected.Spec, current.Spec), nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("timed out waiting for NetworkPolicy %s/%s spec to be restored", namespace, name))
	g.By(fmt.Sprintf("NetworkPolicy %s/%s spec restored after delete", namespace, name))
}

func logNetworkPolicyEvents(ctx context.Context, client k8sclient.Interface, namespaces []string, policyName string) {
	found := false
	_ = wait.PollUntilContextTimeout(ctx, 5*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		for _, namespace := range namespaces {
			eventList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				g.By(fmt.Sprintf("unable to list events in %s: %v", namespace, err))
				continue
			}
			for _, event := range eventList.Items {
				isNPEvent := strings.HasPrefix(event.Reason, "NetworkPolicy") ||
					event.InvolvedObject.Kind == "NetworkPolicy" ||
					(policyName != "" && strings.Contains(event.Message, policyName))
				if isNPEvent {
					g.By(fmt.Sprintf("event in %s: type=%s reason=%s involvedObject=%s/%s message=%q",
						namespace, event.Type, event.Reason,
						event.InvolvedObject.Kind, event.InvolvedObject.Name,
						event.Message))
					found = true
				}
			}
		}
		if found {
			return true, nil
		}
		return false, nil
	})
	if !found {
		g.By(fmt.Sprintf("no NetworkPolicy events observed for %s (best-effort)", policyName))
	}
}

func waitForOperandNetworkPolicy(ctx context.Context, client k8sclient.Interface) {
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := client.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, operandNetworkPolicyName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			klog.Warningf("Error getting NetworkPolicy %s: %v, will retry", operandNetworkPolicyName, err)
			return false, nil
		}
		klog.Infof("NetworkPolicy %s found", operandNetworkPolicyName)
		return true, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("NetworkPolicy %s not found after timeout", operandNetworkPolicyName))
}

func waitForOperandPod(ctx context.Context, client k8sclient.Interface) *corev1.Pod {
	var operandPod *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := client.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=" + operatorclient.OperandName,
		})
		if err != nil {
			return false, nil
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				operandPod = pod.DeepCopy()
				return true, nil
			}
		}
		return false, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), fmt.Sprintf("Unable to get operand pod after timeout"))
	return operandPod
}

func operandClientLabels() map[string]string {
	return map[string]string{operandAppLabelKey: operatorclient.OperandName}
}

func testMutationRecovery(ctx context.Context, kubeClient k8sclient.Interface, patch []byte, timeout time.Duration) {
	original := getNetworkPolicy(ctx, kubeClient, operatorclient.OperatorNamespace, operandNetworkPolicyName)
	_, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Patch(ctx, operandNetworkPolicyName, "application/merge-patch+json", patch, metav1.PatchOptions{})
	if err != nil {
		_, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Patch(ctx, operandNetworkPolicyName, "application/json-patch+json", patch, metav1.PatchOptions{})
	}
	o.Expect(err).NotTo(o.HaveOccurred())

	waitErr := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		current, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, operandNetworkPolicyName, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Error getting NetworkPolicy %s: %v, will retry", operandNetworkPolicyName, err)
			return false, nil
		}
		return apiequality.Semantic.DeepEqual(original.Spec, current.Spec), nil
	})
	o.Expect(waitErr).NotTo(o.HaveOccurred(), "NetworkPolicy spec should be restored after mutation within timeout")

	current, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).Get(ctx, operandNetworkPolicyName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should be able to get NetworkPolicy after restoration")
	o.Expect(original.Spec).To(o.Equal(current.Spec), "Policy spec should be restored after mutation")
}

func scaleDeployment(ctx context.Context, kubeClient k8sclient.Interface, namespace, deploymentName string, replicas int32) {
	deploy, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	deploy.Spec.Replicas = &replicas
	_, err = kubeClient.AppsV1().Deployments(namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
}

func verifyPodCount(ctx context.Context, kubeClient k8sclient.Interface, namespace, labelSelector string, expectedCount int) {
	o.Eventually(func() int {
		pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return -1
		}
		return len(pods.Items)
	}, 1*time.Minute, 2*time.Second).Should(o.Equal(expectedCount), fmt.Sprintf("expected %d pods with selector %s", expectedCount, labelSelector))
}
