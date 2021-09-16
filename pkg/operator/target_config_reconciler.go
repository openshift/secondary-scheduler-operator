package operator

import (
	"context"
	"fmt"
	"reflect"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	openshiftrouteclientset "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/secondary-scheduler-operator/bindata"
	secondaryschedulersv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	operatorconfigclientv1 "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/typed/secondaryscheduler/v1"
	operatorclientinformers "github.com/openshift/secondary-scheduler-operator/pkg/generated/informers/externalversions/secondaryscheduler/v1"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	DefaultImage    = "k8s.gcr.io/scheduler-plugins/kube-scheduler:latest"
	PromNamespace   = "openshift-monitoring"
	PromRouteName   = "prometheus-k8s"
	PromTokenPrefix = "prometheus-k8s-token"
)

// secondarySchedulerCommand provides the scheduler command with configfile mounted as volume and log-level for backwards
// compatibility with 3.11
var secondarySchedulerConfigMap = "secondary-scheduler"

type TargetConfigReconciler struct {
	ctx                      context.Context
	targetImagePullSpec      string
	operatorClient           operatorconfigclientv1.SecondaryschedulersV1Interface
	secondarySchedulerClient *operatorclient.SecondarySchedulerClient
	kubeClient               kubernetes.Interface
	osrClient                openshiftrouteclientset.Interface
	dynamicClient            dynamic.Interface
	eventRecorder            events.Recorder
	queue                    workqueue.RateLimitingInterface
}

func NewTargetConfigReconciler(
	ctx context.Context,
	targetImagePullSpec string,
	operatorConfigClient operatorconfigclientv1.SecondaryschedulersV1Interface,
	operatorClientInformer operatorclientinformers.SecondarySchedulerInformer,
	secondarySchedulerClient *operatorclient.SecondarySchedulerClient,
	kubeClient kubernetes.Interface,
	osrClient openshiftrouteclientset.Interface,
	dynamicClient dynamic.Interface,
	eventRecorder events.Recorder,
) *TargetConfigReconciler {
	c := &TargetConfigReconciler{
		ctx:                      ctx,
		operatorClient:           operatorConfigClient,
		secondarySchedulerClient: secondarySchedulerClient,
		kubeClient:               kubeClient,
		osrClient:                osrClient,
		dynamicClient:            dynamicClient,
		eventRecorder:            eventRecorder,
		queue:                    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TargetConfigReconciler"),
		targetImagePullSpec:      targetImagePullSpec,
	}
	operatorClientInformer.Informer().AddEventHandler(c.eventHandler())

	return c
}

func (c TargetConfigReconciler) sync() error {
	secondaryScheduler, err := c.operatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(c.ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "unable to get operator configuration", "namespace", operatorclient.OperatorNamespace, "secondary-scheduler", operatorclient.OperatorConfigName)
		return err
	}

	forceDeployment := false
	_, forceDeployment, err = c.manageConfigMap(secondaryScheduler)
	if err != nil {
		return err
	}

	if _, _, err := c.manageServiceAccount(secondaryScheduler); err != nil {
		return err
	}

	if _, _, err := c.manageClusterRoleBinding(secondaryScheduler); err != nil {
		return err
	}

	deployment, _, err := c.manageDeployment(secondaryScheduler, forceDeployment)
	if err != nil {
		return err
	}

	_, _, err = v1helpers.UpdateStatus(c.secondarySchedulerClient, func(status *operatorv1.OperatorStatus) error {
		resourcemerge.SetDeploymentGeneration(&status.Generations, deployment)
		return nil
	})
	return err
}

func (c *TargetConfigReconciler) manageConfigMap(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (*v1.ConfigMap, bool, error) {
	var required *v1.ConfigMap
	var err error

	required, err = c.kubeClient.CoreV1().ConfigMaps(secondaryScheduler.Namespace).Get(context.TODO(), string(secondaryScheduler.Spec.SchedulerConfig), metav1.GetOptions{})

	if err != nil {
		klog.Errorf("Cannot load ConfigMap %s for the secondaryscheduler", string(secondaryScheduler.Spec.SchedulerConfig))
		return nil, false, err
	}

	secondarySchedulerConfigMap = string(secondaryScheduler.Spec.SchedulerConfig)
	klog.Infof("Find ConfigMap %s for the secondaryscheduler.", secondaryScheduler.Spec.SchedulerConfig)

	return resourceapply.ApplyConfigMap(c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageServiceAccount(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (*v1.ServiceAccount, bool, error) {
	required := resourceread.ReadServiceAccountV1OrDie(bindata.MustAsset("assets/secondary-scheduler/serviceaccount.yaml"))
	required.Namespace = secondaryScheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "SecondaryScheduler",
			Name:       secondaryScheduler.Name,
			UID:        secondaryScheduler.UID,
		},
	}

	return resourceapply.ApplyServiceAccount(c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageClusterRoleBinding(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/secondary-scheduler/clusterrolebinding.yaml"))
	required.Namespace = secondaryScheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "SecondaryScheduler",
			Name:       secondaryScheduler.Name,
			UID:        secondaryScheduler.UID,
		},
	}

	return resourceapply.ApplyClusterRoleBinding(c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageDeployment(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler, forceDeployment bool) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/secondary-scheduler/deployment.yaml"))
	required.Name = secondaryScheduler.Name
	required.Namespace = secondaryScheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "SecondaryScheduler",
			Name:       secondaryScheduler.Name,
			UID:        secondaryScheduler.UID,
		},
	}

	images := map[string]string{
		"${IMAGE}": c.targetImagePullSpec,
	}
	for i := range required.Spec.Template.Spec.Containers {
		for pat, img := range images {
			if required.Spec.Template.Spec.Containers[i].Image == pat {
				required.Spec.Template.Spec.Containers[i].Image = img
				break
			}
		}
	}

	configmaps := map[string]string{
		"${CONFIGMAP}": secondarySchedulerConfigMap,
	}

	for i := range required.Spec.Template.Spec.Volumes {
		for pat, configmap := range configmaps {
			if required.Spec.Template.Spec.Volumes[i].ConfigMap.Name == pat {
				required.Spec.Template.Spec.Volumes[i].ConfigMap.Name = configmap
				break
			}
		}
	}

	switch secondaryScheduler.Spec.LogLevel {
	case operatorv1.Normal:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 2))
	case operatorv1.Debug:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 4))
	case operatorv1.Trace:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 6))
	case operatorv1.TraceAll:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 8))
	default:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 2))
	}

	if !forceDeployment {
		existingDeployment, err := c.kubeClient.AppsV1().Deployments(required.Namespace).Get(c.ctx, secondaryScheduler.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				forceDeployment = true
			} else {
				return nil, false, err
			}
		} else {
			forceDeployment = deploymentChanged(existingDeployment, required)
		}
	}
	// FIXME: this method will disappear in 4.6 so we need to fix this ASAP
	return resourceapply.ApplyDeploymentWithForce(
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, secondaryScheduler.Status.Generations),
		forceDeployment)
}

func deploymentChanged(existing, new *appsv1.Deployment) bool {
	newArgs := sets.NewString(new.Spec.Template.Spec.Containers[0].Args...)
	existingArgs := sets.NewString(existing.Spec.Template.Spec.Containers[0].Args...)
	return existing.Name != new.Name ||
		existing.Namespace != new.Namespace ||
		existing.Spec.Template.Spec.Containers[0].Image != new.Spec.Template.Spec.Containers[0].Image ||
		existing.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name != new.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name ||
		!reflect.DeepEqual(newArgs, existingArgs)
}

// Run starts the kube-scheduler and blocks until stopCh is closed.
func (c *TargetConfigReconciler) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting TargetConfigReconciler")
	defer klog.Infof("Shutting down TargetConfigReconciler")

	// doesn't matter what workers say, only start one.
	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
}

func (c *TargetConfigReconciler) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *TargetConfigReconciler) processNextWorkItem() bool {
	dsKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(dsKey)

	err := c.sync()
	if err == nil {
		c.queue.Forget(dsKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", dsKey, err))
	c.queue.AddRateLimited(dsKey)

	return true
}

// eventHandler queues the operator to check spec and status
func (c *TargetConfigReconciler) eventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}
