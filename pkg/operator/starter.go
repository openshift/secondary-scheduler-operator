package operator

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	openshiftrouteclientset "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/loglevel"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	operatorconfigclient "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned"
	operatorclientinformers "github.com/openshift/secondary-scheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/configobservation/configobservercontroller"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"
)

const (
	workQueueKey          = "key"
	workQueueCMChangedKey = "CMkey"
)

type queueItem struct {
	kind string
	name string
}

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient,
		"",
		operatorclient.OperatorNamespace,
	)

	operatorConfigClient, err := operatorconfigclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}
	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(operatorConfigClient, 10*time.Minute)
	secondarySchedulerClient := &operatorclient.SecondarySchedulerClient{
		Ctx:            ctx,
		SharedInformer: operatorConfigInformers.Secondaryschedulers().V1().SecondarySchedulers().Informer(),
		OperatorClient: operatorConfigClient.SecondaryschedulersV1(),
	}

	osrClient, err := openshiftrouteclientset.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	configClient, err := configclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}
	configInformers := configinformers.NewSharedInformerFactory(configClient, 10*time.Minute)

	resourceSyncController := resourcesynccontroller.NewResourceSyncController(
		"SecondarySchedulerOperator",
		secondarySchedulerClient,
		kubeInformersForNamespaces,
		kubeClient.CoreV1(),
		kubeClient.CoreV1(),
		cc.EventRecorder,
	)

	configObserver := configobservercontroller.NewConfigObserver(
		secondarySchedulerClient,
		configInformers,
		resourceSyncController,
		cc.EventRecorder,
	)

	targetConfigReconciler, err := NewTargetConfigReconciler(
		ctx,
		operatorConfigClient.SecondaryschedulersV1(),
		operatorConfigInformers.Secondaryschedulers().V1().SecondarySchedulers(),
		kubeInformersForNamespaces,
		secondarySchedulerClient,
		kubeClient,
		osrClient,
		dynamicClient,
		cc.EventRecorder,
	)
	if err != nil {
		return err
	}

	logLevelController := loglevel.NewClusterOperatorLoggingController(secondarySchedulerClient, cc.EventRecorder)

	klog.Infof("Starting informers")
	operatorConfigInformers.Start(ctx.Done())
	kubeInformersForNamespaces.Start(ctx.Done())
	configInformers.Start(ctx.Done())

	klog.Infof("Starting log level controller")
	go logLevelController.Run(ctx, 1)
	klog.Infof("Starting resource sync controller")
	go resourceSyncController.Run(ctx, 1)
	klog.Infof("Starting config observer")
	go configObserver.Run(ctx, 1)
	klog.Infof("Starting target config reconciler")
	go targetConfigReconciler.Run(1, ctx.Done())

	<-ctx.Done()
	return nil
}
