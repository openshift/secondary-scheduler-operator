package e2e

import (
	"os"

	o "github.com/onsi/gomega"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	secondaryschedulerclient "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned"
)

// GetKubeClient returns a Kubernetes clientset or fails the test
func GetKubeClient() *k8sclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := k8sclient.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create kubernetes client")

	return client
}

// GetApiExtensionClient returns an API extension clientset or fails the test
func GetApiExtensionClient() *apiextclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := apiextclientv1.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create API extension client")

	return client
}

// GetSecondarySchedulerClient returns a SecondaryScheduler clientset or fails the test
func GetSecondarySchedulerClient() *secondaryschedulerclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := secondaryschedulerclient.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create SecondaryScheduler client")

	return client
}
