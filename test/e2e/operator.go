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
func setupOperator() (context.Context, context.CancelFunc, *k8sclient.Clientset, error) {
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
			Name:      "test-secondary-scheduler-sheduling-pod",
			Labels:    map[string]string{"app": "test-secondary-scheduler-sheduling"},
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
