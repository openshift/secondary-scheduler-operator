package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	ssv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	ssscheme "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/secondary-scheduler-operator/test/e2e/bindata"
	utilpointer "k8s.io/utils/pointer"

	o "github.com/onsi/gomega"
)

func TestMain(m *testing.M) {
	// Verify required environment variables
	if os.Getenv("KUBECONFIG") == "" {
		klog.Errorf("KUBECONFIG environment variable not set")
		os.Exit(1)
	}
	if os.Getenv("IMAGE") == "" {
		if os.Getenv("IMAGE_FORMAT") == "" {
			klog.Errorf("IMAGE_FORMAT environment variable not set")
			os.Exit(1)
		}
		if os.Getenv("NAMESPACE") == "" {
			klog.Errorf("NAMESPACE environment variable not set")
			os.Exit(1)
		}
	}

	kubeClient := GetKubeClient()
	apiExtClient := GetApiExtensionClient()
	ssClient := GetSecondarySchedulerClient()

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{}, clock.RealClock{})

	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

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
				requiredObj, err := runtime.Decode(ssscheme.Codecs.UniversalDecoder(ssv1.SchemeGroupVersion), objBytes)
				if err != nil {
					klog.Errorf("Unable to decode assets/07_secondary-scheduler-operator.cr.yaml: %v", err)
					return err
				}
				requiredSS := requiredObj.(*ssv1.SecondaryScheduler)

				_, err = ssClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Create(ctx, requiredSS, metav1.CreateOptions{})
				if err == nil {
					return nil
				}
				if !apierrors.IsAlreadyExists(err) {
					return err
				}
				// Get the existing object to obtain its resourceVersion
				existingSS, getErr := ssClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Get(ctx, requiredSS.Name, metav1.GetOptions{})
				if getErr != nil {
					return getErr
				}
				// Update the spec with the required values
				existingSS.Spec = requiredSS.Spec
				_, err = ssClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Update(ctx, existingSS, metav1.UpdateOptions{})
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
	os.Exit(m.Run())
}

func TestScheduling(t *testing.T) {
	kubeClient := GetKubeClient()

	ctx := context.TODO()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
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
	if _, err := kubeClient.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Unable to create a pod: %v", err)
	}

	defer func() {
		wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
			kubeClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			_, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
	}()

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
}
