package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	clocktesting "k8s.io/utils/clock/testing"
	utilpointer "k8s.io/utils/pointer"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	secondaryschedulerv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	secondaryschedulerscheme "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/secondary-scheduler-operator/test/e2e/bindata"

	o "github.com/onsi/gomega"
)

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
			path: "assets/03_clusterrole.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleV1OrDie(objBytes))
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
				ImagePullPolicy: "Always",
				Image:           "kubernetes/pause",
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
	}, time.Minute, 1*time.Second).Should(o.BeTrue(), "pod not running after timeout")

	return testNamespace
}

// cleanupTestNamespace deletes the test namespace.
func cleanupTestNamespace(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, testNamespace string) {
	if testNamespace == "" {
		return
	}
	klog.Infof("Cleaning up test namespace: %s", testNamespace)
	if err := kubeClient.CoreV1().Namespaces().Delete(ctx, testNamespace, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete namespace %s: %v", testNamespace, err)
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
