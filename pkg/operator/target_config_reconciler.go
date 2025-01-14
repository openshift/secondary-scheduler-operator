package operator

import (
	"context"
	"fmt"
	"strconv"
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

	"github.com/openshift/library-go/pkg/controller"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
	ctx                        context.Context
	operatorClient             operatorconfigclientv1.SecondaryschedulersV1Interface
	secondarySchedulerClient   *operatorclient.SecondarySchedulerClient
	kubeClient                 kubernetes.Interface
	osrClient                  openshiftrouteclientset.Interface
	dynamicClient              dynamic.Interface
	eventRecorder              events.Recorder
	queue                      workqueue.RateLimitingInterface
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces
}

func NewTargetConfigReconciler(
	ctx context.Context,
	operatorConfigClient operatorconfigclientv1.SecondaryschedulersV1Interface,
	operatorClientInformer operatorclientinformers.SecondarySchedulerInformer,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	secondarySchedulerClient *operatorclient.SecondarySchedulerClient,
	kubeClient kubernetes.Interface,
	osrClient openshiftrouteclientset.Interface,
	dynamicClient dynamic.Interface,
	eventRecorder events.Recorder,
) (*TargetConfigReconciler, error) {
	c := &TargetConfigReconciler{
		ctx:                        ctx,
		operatorClient:             operatorConfigClient,
		secondarySchedulerClient:   secondarySchedulerClient,
		kubeClient:                 kubeClient,
		osrClient:                  osrClient,
		dynamicClient:              dynamicClient,
		eventRecorder:              eventRecorder,
		queue:                      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TargetConfigReconciler"),
		kubeInformersForNamespaces: kubeInformersForNamespaces,
	}

	_, err := operatorClientInformer.Informer().AddEventHandler(c.eventHandler(queueItem{kind: "secondaryscheduler"}))
	if err != nil {
		return nil, err
	}

	_, err = kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Core().V1().ConfigMaps().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {},
		UpdateFunc: func(old, new interface{}) {
			cm, ok := old.(*v1.ConfigMap)
			if !ok {
				klog.Errorf("Unable to convert obj to ConfigMap")
				return
			}
			c.queue.Add(queueItem{kind: "configmap", name: cm.Name})
		},
		DeleteFunc: func(obj interface{}) {
			cm, ok := obj.(*v1.ConfigMap)
			if !ok {
				klog.Errorf("Unable to convert obj to ConfigMap")
				return
			}
			c.queue.Add(queueItem{kind: "configmap", name: cm.Name})
		},
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c TargetConfigReconciler) sync(item queueItem) error {
	secondaryScheduler, err := c.operatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(c.ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "unable to get operator configuration", "namespace", operatorclient.OperatorNamespace, "secondary-scheduler", operatorclient.OperatorConfigName)
		return err
	}

	specAnnotations := map[string]string{
		"secondaryschedulers.operator.openshift.io/cluster": strconv.FormatInt(secondaryScheduler.Generation, 10),
	}

	// Skip any sync triggered by other than the SchedulerConfig CM changes
	if item.kind == "configmap" {
		if item.name != secondaryScheduler.Spec.SchedulerConfig {
			return nil
		}
		klog.Infof("configmap %q changed, forcing redeployment", secondaryScheduler.Spec.SchedulerConfig)
	}

	configMapResourceVersion, err := c.getConfigMapResourceVersion(secondaryScheduler)
	if err != nil {
		return err
	}
	specAnnotations["configmaps/"+secondaryScheduler.Spec.SchedulerConfig] = configMapResourceVersion

	if sa, _, err := c.manageServiceAccount(secondaryScheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if sa != nil { // SyncConfigMap can return nil
			resourceVersion = sa.ObjectMeta.ResourceVersion
		}
		specAnnotations["serviceaccounts/secondary-scheduler"] = resourceVersion
	}

	if clusterRoleBindings, _, err := c.manageClusterRoleBindings(secondaryScheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterRoleBindings != nil { // SyncConfigMap can return nil
			resourceVersion = clusterRoleBindings.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/secondary-scheduler"] = resourceVersion
	}

	deployment, _, err := c.manageDeployment(secondaryScheduler, specAnnotations)
	if err != nil {
		return err
	}

	_, _, err = v1helpers.UpdateStatus(c.ctx, c.secondarySchedulerClient, func(status *operatorv1.OperatorStatus) error {
		resourcemerge.SetDeploymentGeneration(&status.Generations, deployment)
		return nil
	})
	return err
}

func (c *TargetConfigReconciler) getConfigMapResourceVersion(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (string, error) {
	required, err := c.kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Core().V1().ConfigMaps().Lister().ConfigMaps(operatorclient.OperatorNamespace).Get(secondaryScheduler.Spec.SchedulerConfig)
	if err != nil {
		return "", fmt.Errorf("could not get configuration configmap: %v", err)
	}

	return required.ObjectMeta.ResourceVersion, nil
}

func (c *TargetConfigReconciler) manageServiceAccount(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (*v1.ServiceAccount, bool, error) {
	required := resourceread.ReadServiceAccountV1OrDie(bindata.MustAsset("assets/secondary-scheduler/serviceaccount.yaml"))
	required.Namespace = secondaryScheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "SecondaryScheduler",
		Name:       secondaryScheduler.Name,
		UID:        secondaryScheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyServiceAccount(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageClusterRoleBindings(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/secondary-scheduler/clusterrolebinding-system-kube-scheduler.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "SecondaryScheduler",
		Name:       secondaryScheduler.Name,
		UID:        secondaryScheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	crb, modified, err := resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	if err != nil {
		return crb, modified, err
	}

	required = resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/secondary-scheduler/clusterrolebinding-system-volume-scheduler.yaml"))
	ownerReference = metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "SecondaryScheduler",
		Name:       secondaryScheduler.Name,
		UID:        secondaryScheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageDeployment(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler, specAnnotations map[string]string) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/secondary-scheduler/deployment.yaml"))
	required.Name = operatorclient.OperandName
	required.Namespace = secondaryScheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "SecondaryScheduler",
		Name:       secondaryScheduler.Name,
		UID:        secondaryScheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	images := map[string]string{
		"${IMAGE}": secondaryScheduler.Spec.SchedulerImage,
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
		"${CONFIGMAP}": secondaryScheduler.Spec.SchedulerConfig,
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

	resourcemerge.MergeMap(resourcemerge.BoolPtr(false), &required.Spec.Template.Annotations, specAnnotations)

	return resourceapply.ApplyDeployment(
		c.ctx,
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, secondaryScheduler.Status.Generations))
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
	item := dsKey.(queueItem)
	err := c.sync(item)
	if err == nil {
		c.queue.Forget(dsKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", dsKey, err))
	c.queue.AddRateLimited(dsKey)

	return true
}

// eventHandler queues the operator to check spec and status
func (c *TargetConfigReconciler) eventHandler(item queueItem) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(item) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(item) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(item) },
	}
}
