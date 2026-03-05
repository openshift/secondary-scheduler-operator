package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	utilpointer "k8s.io/utils/pointer"

	o "github.com/onsi/gomega"
)

func TestMain(m *testing.M) {
	_, cancelFnc, _, err := setupOperator()
	if err != nil {
		klog.Errorf("Failed to setup operator: %v", err)
		os.Exit(1)
	}
	defer cancelFnc()

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
