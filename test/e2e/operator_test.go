package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	ssv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	"github.com/openshift/secondary-scheduler-operator/pkg/cmd/operator"
	ssclient "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned"
	ssscheme "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/secondary-scheduler-operator/test/e2e/bindata"
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBECONFIG") == "" {
		klog.Errorf("KUBECONFIG environment variable not set")
		os.Exit(1)
	}

	kubeClient := getKubeClientOrDie()
	apiExtClient := getApiExtensionKubeClient()
	ssClient := getSecondarySchedulerClient()

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{})

	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

	// create required resources, e.g. namespace, crd, roles
	if err := wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
		klog.Infof("Creating assets/00_secondary-scheduler-operator.crd.yaml")
		requiredCRD := resourceread.ReadCustomResourceDefinitionV1OrDie(bindata.MustAsset("assets/00_secondary-scheduler-operator.crd.yaml"))
		if _, _, err := resourceapply.ApplyCustomResourceDefinitionV1(apiExtClient.ApiextensionsV1(), eventRecorder, requiredCRD); err != nil {
			klog.Errorf("Unable to create assets/00_secondary-scheduler-operator.crd.yaml: %v", err)
			return false, nil
		}

		klog.Infof("Creating assets/01_namespace.yaml")
		requiredNS := resourceread.ReadNamespaceV1OrDie(bindata.MustAsset("assets/01_namespace.yaml"))
		if _, _, err := resourceapply.ApplyNamespace(kubeClient.CoreV1(), eventRecorder, requiredNS); err != nil {
			klog.Errorf("Unable to create assets/01_namespace.yaml: %v", err)
			return false, nil
		}

		klog.Infof("Creating assets/assets/06_configmap.yaml")
		requiredCM := resourceread.ReadConfigMapV1OrDie(bindata.MustAsset("assets/06_configmap.yaml"))
		if _, _, err := resourceapply.ApplyConfigMap(kubeClient.CoreV1(), eventRecorder, requiredCM); err != nil {
			klog.Errorf("Unable to create assets/06_configmap.yaml: %v", err)
			return false, nil
		}

		klog.Infof("Creating assets/07_secondary-scheduler-operator.cr.yaml")
		bytesData := bindata.MustAsset("assets/07_secondary-scheduler-operator.cr.yaml")
		requiredObj, err := runtime.Decode(ssscheme.Codecs.UniversalDecoder(ssv1.SchemeGroupVersion), bytesData)
		if err != nil {
			klog.Errorf("Unable to decode assets/07_secondary-scheduler-operator.cr.yaml: %v", err)
			return false, err
		}
		requiredSS := requiredObj.(*ssv1.SecondaryScheduler)

		_, err = ssClient.SecondaryschedulersV1().SecondarySchedulers(requiredSS.Namespace).Create(ctx, requiredSS, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Unable to create secondaryscheduler CR: %v", err)
			return false, nil
		}

		return true, nil
	}); err != nil {
		klog.Errorf("Unable to create SSO resources: %v", err)
		os.Exit(1)
	}

	operatorCmd := operator.NewOperator()
	// TODO(jchaloup): disable the leader election mechanism
	// TODO(jchaloup): redirect the SSO logs into a file?
	operatorCmd.SetArgs([]string{
		"--kubeconfig", os.Getenv("KUBECONFIG"),
		"--namespace", operatorclient.OperatorNamespace,
	})

	go func() {
		if err := operatorCmd.ExecuteContext(ctx); err != nil {
			klog.Errorf("operated executed with error: %v", err)
		}
		os.Exit(1)
	}()

	time.Sleep(5 * time.Second)

	var secondarySchedulerPod *corev1.Pod
	// Wait until the secondary scheduler pod is running
	if err := wait.PollImmediate(5*time.Second, 1*time.Minute, func() (bool, error) {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false, nil
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, operatorclient.OperandName+"-") {
				continue
			}
			klog.Infof("Checking pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				secondarySchedulerPod = pod.DeepCopy()
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		klog.Errorf("Unable to wait for the SS pod to run")
		os.Exit(1)
	}

	klog.Infof("Secondary scheduler running in %v", secondarySchedulerPod.Name)
	os.Exit(m.Run())
}

func TestScheduling(t *testing.T) {
	kubeClient := getKubeClientOrDie()

	ctx := context.TODO()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-secondary-scheduler-sheduling-pod",
			Labels:    map[string]string{"app": "test-secondary-scheduler-sheduling"},
		},
		Spec: corev1.PodSpec{
			SchedulerName: "secondary-scheduler",
			Containers: []corev1.Container{{
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

	if err := wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
		klog.Infof("Listing pods...")
		pod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Unable to get pod: %v", err)
			return false, nil
		}
		if pod.Spec.NodeName == "" {
			klog.Infof("Pod not yet assigned to a node")
			return false, nil
		}
		klog.Infof("Pod successfully assigned to a node: %v", pod.Spec.NodeName)
		return true, nil
	}); err != nil {
		t.Fatalf("Unable to wait for a scheduled pod: %v", err)
	}
}

func getKubeClientOrDie() *k8sclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := k8sclient.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getApiExtensionKubeClient() *apiextclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := apiextclientv1.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getSecondarySchedulerClient() *ssclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := ssclient.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}
