package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

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

	namespace := testScheduling(t, ctx, kubeClient)
	if err := kubeClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete namespace %s: %v", namespace, err)
	}
	o.Eventually(func() bool {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true
		}
		return false
	}, time.Minute, 1*time.Second).Should(o.BeTrue(), "namespace not deleted after timeout")
}
