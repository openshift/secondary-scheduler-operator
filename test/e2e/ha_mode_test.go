package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	o "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	secondaryschedulerv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	secondaryschedulerclient "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
)

// TestHAMode tests the High Availability mode functionality
func TestHAMode(t *testing.T) {
	o.RegisterTestingT(t)

	t.Run("HA Mode Toggle", func(t *testing.T) {
		ctx, cancelFnc, kubeClient, err := setupOperator(t)
		if err != nil {
			t.Fatalf("Failed to setup operator: %v", err)
		}
		defer cancelFnc()

		testHAModeToggle(t, ctx, kubeClient)
	})
}

// testSingleReplicaMode verifies that the secondary scheduler is running in single replica mode
func testSingleReplicaMode(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, secondarySchedulerClient *secondaryschedulerclient.Clientset) {
	klog.Infof("Verifying single replica mode")

	// Get the current SecondaryScheduler CR
	ss, err := secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(operatorclient.OperatorNamespace).
		Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should get SecondaryScheduler CR")

	// Verify HA mode is not enabled (can be empty string which defaults to SingleReplica, or explicitly SingleReplica)
	o.Expect(ss.Spec.Topology.Mode).To(o.Or(
		o.BeEmpty(),
		o.Equal(secondaryschedulerv1.SingleReplicaMode),
	), "topology mode should be empty (defaults to SingleReplica) or SingleReplica")

	// Verify deployment has only 1 replica
	o.Eventually(func() bool {
		deployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).
			Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil && apierrors.IsNotFound(err) {
			return false
		}
		o.Expect(err).NotTo(o.HaveOccurred(), "should get deployment")
		o.Expect(deployment.Spec.Replicas).NotTo(o.BeNil(), "deployment replicas should not be nil")
		o.Expect(*deployment.Spec.Replicas).To(o.Equal(int32(1)), "deployment should have 1 replica")

		return true
	}, time.Minute, 5*time.Second).Should(o.BeTrue(), "a deployment should be found")

	// Verify only 1 pod is running and ready
	o.Eventually(func() bool {
		pods, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=secondary-scheduler",
		})
		if err != nil {
			klog.Errorf("Failed to list pods: %v", err)
			return false
		}

		runningCount := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				// Check if pod is ready
				ready := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						ready = true
						break
					}
				}
				if ready {
					runningCount++
				}
			}
		}

		klog.Infof("Running and ready pods: %d/1", runningCount)
		return runningCount == 1
	}, 2*time.Minute, 5*time.Second).Should(o.BeTrue(), "exactly 1 pod should be running and ready")

	klog.Infof("Single replica mode verified successfully")
}

// testHAModeToggle tests enabling and disabling HA mode
func testHAModeToggle(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	secondarySchedulerClient := GetSecondarySchedulerClient()

	// Verify initial state: single replica mode
	testSingleReplicaMode(t, ctx, kubeClient, secondarySchedulerClient)

	// Get the current SecondaryScheduler CR for update
	ss, err := secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(operatorclient.OperatorNamespace).
		Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should get SecondaryScheduler CR")

	klog.Infof("Enabling HA mode on SecondaryScheduler CR")

	// Enable HA mode
	ss.Spec.Topology.Mode = secondaryschedulerv1.HighlyAvailableMode
	// Initialize HighlyAvailableTopology and set MaxReplicas to 3
	ss.Spec.Topology.HighlyAvailableTopology = &secondaryschedulerv1.HighlyAvailableTopology{
		MaxReplicas: 3,
	}

	_, err = secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(operatorclient.OperatorNamespace).
		Update(ctx, ss, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should update SecondaryScheduler CR to enable HA mode")

	// Wait for deployment to scale to 3 replicas and all pods to be running
	klog.Infof("Waiting for deployment to scale to 3 replicas and all pods to be running")
	var currentReplicaSetName string
	o.Eventually(func() bool {
		// Get deployment and check replicas
		deployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).
			Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get deployment: %v", err)
			return false
		}

		if deployment.Spec.Replicas == nil {
			klog.Infof("Deployment replicas is nil")
			return false
		}

		replicas := *deployment.Spec.Replicas
		if replicas != 3 {
			klog.Infof("Current deployment replicas: %d, waiting for 3", replicas)
			return false
		}

		// Get the deployment's current revision
		deploymentRevision, ok := deployment.Annotations["deployment.kubernetes.io/revision"]
		if !ok {
			klog.Infof("Deployment does not have revision annotation yet")
			return false
		}

		// Get the current replicaset for this deployment
		replicaSets, err := kubeClient.AppsV1().ReplicaSets(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=secondary-scheduler",
		})
		if err != nil {
			klog.Errorf("Failed to list replicasets: %v", err)
			return false
		}

		// Find the replicaset with matching revision annotation
		currentReplicaSetName = ""
		for _, rs := range replicaSets.Items {
			rsRevision, ok := rs.Annotations["deployment.kubernetes.io/revision"]
			if !ok {
				continue
			}
			if rsRevision == deploymentRevision && rs.Spec.Replicas != nil && *rs.Spec.Replicas == replicas {
				currentReplicaSetName = rs.Name
				klog.Infof("Found current replicaset: %s with revision %s and %d desired replicas", currentReplicaSetName, rsRevision, replicas)
				break
			}
		}

		if currentReplicaSetName == "" {
			klog.Infof("Current replicaset not found yet")
			return false
		}

		// Count running and ready pods from the current replicaset
		pods, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=secondary-scheduler",
		})
		if err != nil {
			klog.Errorf("Failed to list pods: %v", err)
			return false
		}

		runningCount := 0
		for _, pod := range pods.Items {
			// Only count pods from the current replicaset
			if !strings.HasPrefix(pod.Name, currentReplicaSetName) {
				continue
			}

			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				// Check if pod is ready
				ready := false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						ready = true
						break
					}
				}
				if ready {
					runningCount++
				}
			}
		}

		klog.Infof("Running and ready pods from replicaset %s: %d/3", currentReplicaSetName, runningCount)
		return runningCount == 3
	}, 4*time.Minute, 5*time.Second).Should(o.BeTrue(), "deployment should have 3 replicas and all pods should be running and ready")

	klog.Infof("All 3 pods from replicaset %s are running and ready", currentReplicaSetName)

	// Get all running pods from the current replicaset and verify pod anti-affinity
	klog.Infof("Verifying pod anti-affinity configuration on all pods from replicaset %s", currentReplicaSetName)
	pods, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=secondary-scheduler",
	})
	o.Expect(err).NotTo(o.HaveOccurred(), "should list pods")

	runningPods := []corev1.Pod{}
	for _, pod := range pods.Items {
		// Only include pods from the current replicaset
		if !strings.HasPrefix(pod.Name, currentReplicaSetName) {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
			runningPods = append(runningPods, pod)
		}
	}

	o.Expect(runningPods).To(o.HaveLen(3), "should have exactly 3 running pods from current replicaset")

	// Verify each pod has the expected pod anti-affinity configuration
	for i, pod := range runningPods {
		klog.Infof("Checking pod anti-affinity for pod %d: %s", i+1, pod.Name)

		o.Expect(pod.Spec.Affinity).NotTo(o.BeNil(), "pod %s should have affinity configured", pod.Name)
		o.Expect(pod.Spec.Affinity.PodAntiAffinity).NotTo(o.BeNil(), "pod %s should have pod anti-affinity configured", pod.Name)

		preferredAntiAffinity := pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
		o.Expect(preferredAntiAffinity).NotTo(o.BeEmpty(), "pod %s should have preferred anti-affinity rules", pod.Name)

		// Verify the anti-affinity rule
		found := false
		for _, rule := range preferredAntiAffinity {
			if rule.Weight == 100 &&
				rule.PodAffinityTerm.TopologyKey == "kubernetes.io/hostname" {
				// Check label selector
				if rule.PodAffinityTerm.LabelSelector != nil &&
					rule.PodAffinityTerm.LabelSelector.MatchLabels != nil {
					if appLabel, ok := rule.PodAffinityTerm.LabelSelector.MatchLabels["app"]; ok && appLabel == "secondary-scheduler" {
						found = true
						klog.Infof("Pod %s has correct anti-affinity rule", pod.Name)
						break
					}
				}
			}
		}

		o.Expect(found).To(o.BeTrue(), "pod %s should have anti-affinity rule with weight=100, topologyKey=kubernetes.io/hostname, and app=secondary-scheduler label selector", pod.Name)
	}

	klog.Infof("All pods have correct pod anti-affinity configuration")

	// Disable HA mode
	klog.Infof("Disabling HA mode on SecondaryScheduler CR")
	ss, err = secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(operatorclient.OperatorNamespace).
		Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should get SecondaryScheduler CR")

	ss.Spec.Topology.Mode = secondaryschedulerv1.SingleReplicaMode
	ss.Spec.Topology.HighlyAvailableTopology = nil

	_, err = secondarySchedulerClient.SecondaryschedulersV1().SecondarySchedulers(operatorclient.OperatorNamespace).
		Update(ctx, ss, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should update SecondaryScheduler CR to disable HA mode")

	// Wait for deployment to be updated to 1 replica
	klog.Infof("Waiting for deployment to scale down to 1 replica")
	o.Eventually(func() bool {
		deployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).
			Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Failed to get deployment: %v", err)
			return false
		}

		if deployment.Spec.Replicas == nil {
			klog.Infof("Deployment replicas is nil")
			return false
		}

		replicas := *deployment.Spec.Replicas
		klog.Infof("Current deployment replicas after disabling HA: %d", replicas)
		return replicas == 1
	}, 2*time.Minute, 5*time.Second).Should(o.BeTrue(), "deployment should have 1 replica after disabling HA mode")

	// Verify we're back to single replica mode
	testSingleReplicaMode(t, ctx, kubeClient, secondarySchedulerClient)

	klog.Infof("HA mode toggle test completed successfully")
}
